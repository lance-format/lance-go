package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	defaultHold         = 30 * time.Second
	defaultFetchTimeout = 30 * time.Second
	minDeadline         = 10 * time.Second
	maxDeadline         = 2 * time.Hour
	maxHold             = 10 * time.Minute
	maxFetchTimeout     = 2 * time.Minute
	verificationReserve = 30 * time.Second
	maxPrefixBytes      = 512
)

var (
	regionPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)
	bucketPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	prefixSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

type config struct {
	uri                  string
	region               string
	credentialExecutable string
	credentialArgs       []string
	deadline             time.Duration
	hold                 time.Duration
	fetchTimeout         time.Duration
	reportPath           string
}

type targetPrefix struct {
	bucket string
	path   string
}

func (p targetPrefix) withSuffix(suffix string) string {
	return "s3://" + p.bucket + "/" + p.path + "/" + suffix
}

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, " ")
}

func (s *stringList) Set(value string) error {
	if strings.ContainsRune(value, 0) {
		return errors.New("argument must not contain NUL")
	}
	*s = append(*s, value)
	return nil
}

func parseConfig(args []string) (config, targetPrefix, error) {
	var cfg config
	var credentialArgs stringList

	flags := flag.NewFlagSet("cloud-refresh", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&cfg.uri, "uri", "", "explicit s3://bucket/non-root-prefix dataset parent")
	flags.StringVar(&cfg.region, "region", "", "AWS region for the bucket")
	flags.StringVar(&cfg.credentialExecutable, "credential-executable", "", "operator executable implementing AWS credential_process v1")
	flags.Var(&credentialArgs, "credential-arg", "argument passed to the credential executable; repeat for each argument")
	flags.DurationVar(&cfg.deadline, "deadline", 0, "bounded total run deadline")
	flags.DurationVar(&cfg.hold, "hold", defaultHold, "time to wait after A is seeded before forcing the native refresh")
	flags.DurationVar(&cfg.fetchTimeout, "fetch-timeout", defaultFetchTimeout, "maximum time for one credential executable invocation")
	flags.StringVar(&cfg.reportPath, "report", "-", "report path, or - for stdout")

	if err := flags.Parse(args); err != nil {
		return config{}, targetPrefix{}, errors.New("invalid command line")
	}
	if flags.NArg() != 0 {
		return config{}, targetPrefix{}, errors.New("unexpected positional arguments; pass credential arguments with --credential-arg")
	}
	cfg.credentialArgs = []string(credentialArgs)

	if cfg.uri == "" {
		return config{}, targetPrefix{}, errors.New("missing required --uri")
	}
	prefix, err := parseTargetPrefix(cfg.uri)
	if err != nil {
		return config{}, targetPrefix{}, fmt.Errorf("--uri: %w", err)
	}

	if cfg.region == "" {
		return config{}, targetPrefix{}, errors.New("missing required --region")
	}
	if !regionPattern.MatchString(cfg.region) {
		return config{}, targetPrefix{}, errors.New("--region must be a lowercase AWS region such as us-west-2")
	}

	if cfg.credentialExecutable == "" {
		return config{}, targetPrefix{}, errors.New("missing required --credential-executable")
	}
	resolved, err := exec.LookPath(cfg.credentialExecutable)
	if err != nil {
		return config{}, targetPrefix{}, errors.New("--credential-executable was not found or is not executable")
	}
	cfg.credentialExecutable = resolved

	if cfg.deadline == 0 {
		return config{}, targetPrefix{}, errors.New("missing required --deadline")
	}
	if cfg.deadline < minDeadline {
		return config{}, targetPrefix{}, fmt.Errorf("--deadline must be at least %s", minDeadline)
	}
	if cfg.deadline > maxDeadline {
		return config{}, targetPrefix{}, fmt.Errorf("--deadline must be at most %s", maxDeadline)
	}
	if cfg.hold < 5*time.Second || cfg.hold > maxHold {
		return config{}, targetPrefix{}, fmt.Errorf("--hold must be at least 5s and at most %s", maxHold)
	}
	if cfg.fetchTimeout <= 0 || cfg.fetchTimeout > maxFetchTimeout {
		return config{}, targetPrefix{}, fmt.Errorf("--fetch-timeout must be greater than zero and at most %s", maxFetchTimeout)
	}
	if cfg.hold+cfg.fetchTimeout+verificationReserve > cfg.deadline {
		return config{}, targetPrefix{}, fmt.Errorf("--deadline must cover --hold (%s), --fetch-timeout (%s), and a %s verification reserve", cfg.hold, cfg.fetchTimeout, verificationReserve)
	}
	if cfg.reportPath == "" {
		cfg.reportPath = "-"
	}

	return cfg, prefix, nil
}

func parseTargetPrefix(raw string) (targetPrefix, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return targetPrefix{}, errors.New("must be a valid s3:// URI")
	}
	if u.Scheme != "s3" {
		return targetPrefix{}, errors.New("must use the s3:// scheme")
	}
	if u.Host == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return targetPrefix{}, errors.New("must be s3://bucket/non-root-prefix without user, query, or fragment")
	}
	if u.RawPath != "" {
		return targetPrefix{}, errors.New("percent-escaped path segments are not supported")
	}
	if !bucketPattern.MatchString(u.Host) || strings.Contains(u.Host, "..") || strings.Contains(u.Host, ".-") || strings.Contains(u.Host, "-.") {
		return targetPrefix{}, errors.New("bucket name is not a valid AWS S3 bucket name")
	}

	if u.Path == "" || u.Path == "/" {
		return targetPrefix{}, errors.New("prefix must not be the bucket root")
	}
	if !strings.HasPrefix(u.Path, "/") || strings.HasSuffix(u.Path, "/") || strings.Contains(u.Path, "//") {
		return targetPrefix{}, errors.New("prefix must not have leading, trailing, or repeated slashes")
	}
	path := strings.TrimPrefix(u.Path, "/")
	if len(path) > maxPrefixBytes {
		return targetPrefix{}, fmt.Errorf("prefix must be at most %d bytes", maxPrefixBytes)
	}
	segments := strings.Split(path, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return targetPrefix{}, errors.New("prefix must not contain empty, . or .. segments")
		}
		if !prefixSegmentPattern.MatchString(segment) {
			return targetPrefix{}, errors.New("prefix segments may contain only letters, digits, dot, underscore, and hyphen")
		}
	}
	return targetPrefix{bucket: u.Host, path: path}, nil
}
