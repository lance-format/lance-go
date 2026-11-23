// Command cloud writes and reopens a dataset through static object-store
// options. Pass credentials and endpoints on the command line or through the
// environment; this example never stores or prints them.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/examples/internal/exampleutil"
)

type storageOptionsFlag map[string]string

func (storageOptionsFlag) String() string { return "" }

func (o storageOptionsFlag) Set(value string) error {
	key, option, ok := strings.Cut(value, "=")
	if !ok || key == "" {
		return fmt.Errorf("storage option must be key=value")
	}
	o[key] = option
	return nil
}

func main() {
	uri := flag.String("uri", "", "dataset URI, such as s3://bucket/prefix/data.lance or a local path")
	mode := flag.String("mode", "create", "create a new dataset or open an existing one")
	timeout := flag.Duration("timeout", 2*time.Minute, "overall operation timeout")
	options := storageOptionsFlag{}
	flag.Var(options, "storage-option", "object-store option as key=value; repeat for each option")
	flag.Parse()

	if err := run(*uri, *mode, *timeout, options); err != nil {
		log.Fatal(err)
	}
}

func run(uri, mode string, timeout time.Duration, storageOptions map[string]string) error {
	if uri == "" {
		return fmt.Errorf("missing -uri")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var openOptions []lance.OpenOption
	var writeOptions []lance.WriteOption
	if len(storageOptions) > 0 {
		openOptions = append(openOptions, lance.WithStorageOptions(storageOptions))
		writeOptions = append(writeOptions, lance.WithWriteStorageOptions(storageOptions))
	}

	var dataset *lance.Dataset
	var err error
	switch mode {
	case "create":
		schema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		}, nil)
		reader, err := exampleutil.NewReader(schema, func(builder *array.RecordBuilder) {
			builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3}, nil)
		})
		if err != nil {
			return err
		}
		defer reader.Release()

		dataset, err = lance.Create(ctx, uri, reader, writeOptions...)
		if err != nil {
			return err
		}
		if err := dataset.Close(); err != nil {
			return err
		}
		dataset, err = lance.Open(ctx, uri, openOptions...)
		if err != nil {
			return err
		}
	case "open":
		dataset, err = lance.Open(ctx, uri, openOptions...)
	default:
		return fmt.Errorf("-mode must be create or open")
	}
	if err != nil {
		return err
	}
	defer dataset.Close()

	ids, err := exampleutil.ScanInt64(ctx, dataset, "id")
	if err != nil {
		return err
	}
	slices.Sort(ids)
	fmt.Println("cloud rows:", ids)
	return nil
}
