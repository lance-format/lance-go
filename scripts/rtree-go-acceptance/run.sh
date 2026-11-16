#!/usr/bin/env bash
set -u -o pipefail

readonly CANDIDATE_COMMIT="875d712c48bbe71ff68cb7afc9aa1f5bb6c16318"
readonly CANDIDATE_TREE="869b5e9df8dfc3e4f172dad8e17a7e0ea3cbefe3"
readonly BASE_COMMIT="2d02950e0ada4c3b9b3604b5cc9e8b68edcff3dc"
readonly CANDIDATE_BRANCH="xuanwo/add-rtree-scalar-index"
readonly TRANSPORT_CHANGED_FILE="scripts/rtree-go-acceptance/run.sh"

readonly CANDIDATE_PREFIX="/home/xuanwo/Code/lance-c-validation/rtree149-beta20-c3fb417-artifact-20261116/candidate-c3fb417"
readonly CANDIDATE_LIB="${CANDIDATE_PREFIX}/lib/liblance_c.so"
readonly CANDIDATE_HEADER="${CANDIDATE_PREFIX}/include/lance/lance.h"
readonly LEGACY_LIB="/home/xuanwo/Code/lance-go-acceptance/artifacts/native-ac2634c/stripped/liblance_c.so"

readonly EXPECTED_CANDIDATE_LIB_SHA="ca03b12e10d74cd27a9f5ce4bca6eacd27fdafa1851cac99cac9e0c565581ca6"
readonly EXPECTED_CANDIDATE_HEADER_SHA="99c36cfa1ce40e56f06582b3632c4fa66587998329259227051d33b4d1036f39"
readonly EXPECTED_LEGACY_LIB_SHA="e5571d6da16e81a0434f2509ade5262785b63df0b37ff7155537642841dd9378"

readonly GO_BIN="/home/xuanwo/Code/tools/go/bin/go"
readonly GOFMT_BIN="/home/xuanwo/Code/tools/go/bin/gofmt"
readonly GIT_BIN="/usr/bin/git"
readonly JQ_BIN="/home/xuanwo/.local/bin/jq"
readonly SHA256SUM_BIN="/usr/bin/sha256sum"

repo="${1:-}"
out="${2:-}"
if [[ $# -ne 2 || -z "${repo}" || -z "${out}" ]]; then
  printf 'usage: %s <repo> <evidence-dir>\n' "$0" >&2
  exit 64
fi

parent_dir="$(dirname -- "${out}")"
mkdir -p -- "${parent_dir}" || exit 64
if [[ -e "${out}" ]]; then
  printf 'evidence path already exists: %s\n' "${out}" >&2
  exit 64
fi
mkdir -- "${out}" || exit 64
exec >>"${out}/runner.log" 2>&1

overall_exit=0
first_failure=""
tests_allowed=true
gofmt_exit=-1
gofmt_diff_bytes=0
gofmt_clean=false
hashes_before_ok=false
hashes_after_same=false
hashes_after_expected=false
source_clean_before=false
source_clean_after=false

now_utc() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

now_beijing() {
  TZ=Asia/Shanghai date +%Y-%m-%dT%H:%M:%S+08:00
}

log() {
  printf '[%s] %s\n' "$(now_utc)" "$*"
}

record_failure() {
  if [[ -z "${first_failure}" ]]; then
    first_failure="$1"
    log "first failure: ${first_failure}"
  fi
  overall_exit=1
}

write_value() {
  local file="$1"
  shift
  printf '%s\n' "$*" >"${file}.tmp.$$" && mv -- "${file}.tmp.$$" "${file}"
}

log "starting bounded RTree acceptance"
log "transport commit: $("${GIT_BIN}" -C "${repo}" rev-parse HEAD 2>/dev/null || true)"
log "evidence: ${out}"

if [[ ! -d "${repo}/.git" ]]; then
  record_failure "repository is not a Git checkout: ${repo}"
  tests_allowed=false
fi
for tool in "${GIT_BIN}" "${JQ_BIN}" "${SHA256SUM_BIN}" "${GO_BIN}" "${GOFMT_BIN}"; do
  if [[ ! -x "${tool}" ]]; then
    record_failure "required executable is missing: ${tool}"
    tests_allowed=false
  fi
done

transport_commit=""
transport_parent=""
transport_parent_tree=""
transport_changed_files=""
remote_ref_at_start=""
if [[ -d "${repo}/.git" ]]; then
  transport_commit="$("${GIT_BIN}" -C "${repo}" rev-parse HEAD 2>/dev/null || true)"
  transport_parent="$("${GIT_BIN}" -C "${repo}" rev-parse 'HEAD^' 2>/dev/null || true)"
  transport_parent_tree="$("${GIT_BIN}" -C "${repo}" rev-parse 'HEAD^^{tree}' 2>/dev/null || true)"
  transport_changed_files="$("${GIT_BIN}" -C "${repo}" diff --name-only 'HEAD^' HEAD 2>/dev/null || true)"
  remote_ref_at_start="$("${GIT_BIN}" -C "${repo}" ls-remote origin "refs/heads/${CANDIDATE_BRANCH}" 2>/dev/null | awk 'NR == 1 {print $1}')"
  "${GIT_BIN}" -C "${repo}" status --porcelain=v1 --untracked-files=all >"${out}/source-status-before.txt" 2>"${out}/source-status-before.stderr" || true
  if [[ ! -s "${out}/source-status-before.txt" ]]; then
    source_clean_before=true
  else
    record_failure "source checkout was not clean before execution"
    tests_allowed=false
  fi
fi

if [[ "${transport_parent}" != "${CANDIDATE_COMMIT}" ]]; then
  record_failure "transport parent ${transport_parent:-missing} does not match candidate ${CANDIDATE_COMMIT}"
  tests_allowed=false
fi
if [[ "${transport_parent_tree}" != "${CANDIDATE_TREE}" ]]; then
  record_failure "candidate tree ${transport_parent_tree:-missing} does not match ${CANDIDATE_TREE}"
  tests_allowed=false
fi
if [[ "${transport_changed_files}" != "${TRANSPORT_CHANGED_FILE}" ]]; then
  record_failure "transport commit changed unexpected files: ${transport_changed_files:-missing}"
  tests_allowed=false
fi
if [[ "${remote_ref_at_start}" != "${CANDIDATE_COMMIT}" ]]; then
  record_failure "candidate branch ref ${remote_ref_at_start:-missing} does not match ${CANDIDATE_COMMIT}"
  tests_allowed=false
fi
if ! "${GIT_BIN}" -C "${repo}" merge-base --is-ancestor "${BASE_COMMIT}" "${CANDIDATE_COMMIT}" 2>/dev/null; then
  record_failure "base ${BASE_COMMIT} is not an ancestor of candidate ${CANDIDATE_COMMIT}"
  tests_allowed=false
fi

go_version=""
if [[ -x "${GO_BIN}" ]]; then
  go_version="$("${GO_BIN}" version 2>&1 || true)"
  GOTOOLCHAIN=local "${GO_BIN}" env -json GOOS GOARCH GOVERSION GOTOOLCHAIN CGO_ENABLED GOCACHE GOMODCACHE >"${out}/go-env.json" 2>"${out}/go-env.stderr" || record_failure "could not read Go environment"
fi
{
  printf 'utc_start=%s\n' "$(now_utc)"
  printf 'beijing_start=%s\n' "$(now_beijing)"
  printf 'host=%s\n' "$(hostname)"
  printf 'kernel=%s\n' "$(uname -sr)"
  printf 'architecture=%s\n' "$(uname -m)"
  printf 'cpu_count=%s\n' "$(nproc)"
  printf 'go_path=%s\n' "${GO_BIN}"
  printf 'go_version=%s\n' "${go_version}"
  printf 'gofmt_path=%s\n' "${GOFMT_BIN}"
  printf 'git_path=%s\n' "${GIT_BIN}"
  printf 'git_version=%s\n' "$("${GIT_BIN}" --version 2>&1 || true)"
} >"${out}/environment.txt"

"${JQ_BIN}" -n '{
  exit_code: -1,
  file_count: 0,
  diff_bytes: 0,
  diff_sha256: "",
  clean: false
}' >"${out}/gofmt-summary.json"

"${JQ_BIN}" -n \
  --arg candidate_commit "${CANDIDATE_COMMIT}" \
  --arg candidate_tree "${CANDIDATE_TREE}" \
  --arg base_commit "${BASE_COMMIT}" \
  --arg candidate_branch "${CANDIDATE_BRANCH}" \
  --arg remote_ref_at_start "${remote_ref_at_start}" \
  --arg transport_commit "${transport_commit}" \
  --arg transport_parent "${transport_parent}" \
  --arg transport_parent_tree "${transport_parent_tree}" \
  --arg transport_changed_files "${transport_changed_files}" \
  --argjson source_clean_before "${source_clean_before}" \
  '{
    candidate_commit: $candidate_commit,
    candidate_tree: $candidate_tree,
    base_commit: $base_commit,
    candidate_branch: $candidate_branch,
    remote_ref_at_start: $remote_ref_at_start,
    transport_commit: $transport_commit,
    transport_parent: $transport_parent,
    transport_parent_tree: $transport_parent_tree,
    transport_changed_files: ($transport_changed_files | split("\n") | map(select(length > 0))),
    source_clean_before: $source_clean_before
  }' >"${out}/source-identity.json.tmp.$$" &&
  mv -- "${out}/source-identity.json.tmp.$$" "${out}/source-identity.json" ||
  record_failure "could not write source identity evidence"

hash_specs=(
  "candidate-library|${CANDIDATE_LIB}|${EXPECTED_CANDIDATE_LIB_SHA}"
  "candidate-header|${CANDIDATE_HEADER}|${EXPECTED_CANDIDATE_HEADER_SHA}"
  "legacy-library|${LEGACY_LIB}|${EXPECTED_LEGACY_LIB_SHA}"
)

record_hashes() {
  local name="$1"
  local file="${out}/${name}"
  local tmp="${file}.tmp.$$"
  local spec label path expected sha size
  : >"${tmp}" || return 1
  for spec in "${hash_specs[@]}"; do
    IFS='|' read -r label path expected <<<"${spec}"
    if [[ ! -f "${path}" ]]; then
      log "missing library input ${label}: ${path}"
      rm -f -- "${tmp}"
      return 1
    fi
    log "hashing ${label}: ${path}"
    sha="$("${SHA256SUM_BIN}" -- "${path}" | awk 'NR == 1 {print $1}')" || {
      rm -f -- "${tmp}"
      return 1
    }
    size="$(stat -c '%s' -- "${path}")" || {
      rm -f -- "${tmp}"
      return 1
    }
    printf '%s\t%s\t%s\t%s\t%s\n' "${label}" "${path}" "${size}" "${expected}" "${sha}" >>"${tmp}" || {
      rm -f -- "${tmp}"
      return 1
    }
  done
  mv -- "${tmp}" "${file}"
}

validate_hashes_expected() {
  local file="$1"
  local label path size expected actual
  while IFS=$'\t' read -r label path size expected actual; do
    if [[ "${actual}" != "${expected}" ]]; then
      log "hash mismatch for ${label}: expected ${expected}, got ${actual}"
      return 1
    fi
  done <"${file}"
}

if record_hashes "hashes-before.tsv"; then
  if validate_hashes_expected "${out}/hashes-before.tsv"; then
    hashes_before_ok=true
  else
    record_failure "consumed library/header hashes do not match retained identities"
    tests_allowed=false
  fi
else
  record_failure "could not hash consumed library/header inputs before execution"
  tests_allowed=false
fi

if [[ "${tests_allowed}" == true ]]; then
  mapfile -t go_files < <("${GIT_BIN}" -C "${repo}" diff --name-only "${BASE_COMMIT}" "${CANDIDATE_COMMIT}" -- '*.go' | LC_ALL=C sort)
  printf '%s\n' "${go_files[@]}" >"${out}/gofmt-files.txt"
  log "running one gofmt -d over ${#go_files[@]} changed Go files"
  if [[ ${#go_files[@]} -eq 0 ]]; then
    record_failure "no changed Go files found for gofmt validation"
    tests_allowed=false
  else
    (
      cd "${repo}" || exit 1
      "${GOFMT_BIN}" -d "${go_files[@]}"
    ) >"${out}/gofmt.diff" 2>"${out}/gofmt.stderr"
    gofmt_exit=$?
    gofmt_diff_bytes="$(stat -c '%s' "${out}/gofmt.diff")"
    if [[ "${gofmt_diff_bytes}" -eq 0 ]]; then
      gofmt_clean=true
    fi
    gofmt_diff_sha="$("${SHA256SUM_BIN}" -- "${out}/gofmt.diff" | awk 'NR == 1 {print $1}')"
    write_value "${out}/gofmt.exit" "${gofmt_exit}"
    "${JQ_BIN}" -n \
      --argjson exit_code "${gofmt_exit}" \
      --argjson file_count "${#go_files[@]}" \
      --argjson diff_bytes "${gofmt_diff_bytes}" \
      --arg diff_sha256 "${gofmt_diff_sha}" \
      --argjson clean "${gofmt_clean}" \
      '{
        exit_code: $exit_code,
        file_count: $file_count,
        diff_bytes: $diff_bytes,
        diff_sha256: $diff_sha256,
        clean: $clean
      }' >"${out}/gofmt-summary.json.tmp.$$" &&
      mv -- "${out}/gofmt-summary.json.tmp.$$" "${out}/gofmt-summary.json" ||
      record_failure "could not write gofmt summary"
    if [[ "${gofmt_exit}" -ne 0 ]]; then
      record_failure "gofmt -d exited ${gofmt_exit}"
      tests_allowed=false
    fi
  fi
fi

summarize_test() {
  local id="$1"
  local selected="$2"
  local exit_code="$3"
  local duration_seconds="$4"
  local json="${out}/${id}.json"
  local summary="${out}/${id}.summary.json"
  local tmp="${summary}.tmp.$$"
  local selected_name="${selected#^}"
  selected_name="${selected_name%$}"
  if ! "${JQ_BIN}" -s \
    --arg id "${id}" \
    --arg selector "${selected}" \
    --arg selected "${selected_name}" \
    --argjson exit_code "${exit_code}" \
    --argjson duration_seconds "${duration_seconds}" \
    '
      def unique_names($action):
        [.[] | select(.Action == $action and .Test != null) | .Test] | unique;
      (unique_names("run")) as $run |
      (unique_names("pass")) as $pass |
      (unique_names("fail")) as $fail |
      (unique_names("skip")) as $skip |
      {
        id: $id,
        selector: $selector,
        selected_test: $selected,
        exit_code: $exit_code,
        duration_seconds: $duration_seconds,
        json_events: length,
        run_count: ($run | length),
        pass_count: ($pass | length),
        fail_count: ($fail | length),
        skip_count: ($skip | length),
        top_level_run_count: ([$run[] | select(test("/") | not)] | length),
        subtest_run_count: ([$run[] | select(test("/"))] | length),
        selected_test_ran: (($run | index($selected)) != null),
        selected_test_passed: (($pass | index($selected)) != null),
        selected_test_skipped: (($skip | index($selected)) != null),
        any_skip: (($skip | length) > 0),
        any_fail: (($fail | length) > 0)
      }
    ' "${json}" >"${tmp}"; then
    rm -f -- "${tmp}"
    record_failure "${id}: go test JSON could not be parsed"
    return 1
  fi
  mv -- "${tmp}" "${summary}" || {
    record_failure "${id}: could not retain parsed JSON summary"
    return 1
  }
  if ! "${JQ_BIN}" -e '
    .exit_code == 0 and
    .selected_test_ran and
    .selected_test_passed and
    (.selected_test_skipped | not) and
    (.any_skip | not) and
    (.any_fail | not)
  ' "${summary}" >/dev/null; then
    log "selected test contract failed for ${id}"
    return 1
  fi
}

run_test() {
  local id="$1"
  local tags="$2"
  local selector="$3"
  local package="$4"
  local library="$5"
  local json="${out}/${id}.json"
  local stderr_file="${out}/${id}.stderr"
  local command_file="${out}/${id}.command.txt"
  local exit_file="${out}/${id}.exit"
  local -a cmd

  if [[ "${library}" == "-" ]]; then
    cmd=(env -u LANCE_LIB_PATH CGO_ENABLED=0 GOTOOLCHAIN=local "${GO_BIN}" test)
  else
    cmd=(env CGO_ENABLED=0 GOTOOLCHAIN=local "LANCE_LIB_PATH=${library}" "${GO_BIN}" test)
  fi
  if [[ "${tags}" != "-" ]]; then
    cmd+=(-tags "${tags}")
  fi
  cmd+=(-run "${selector}" -count=1 -json "${package}")

  {
    printf '%s\n' "${id}:"
    printf '%q ' "${cmd[@]}"
    printf '\n'
  } >>"${out}/commands.txt"
  printf '%q ' "${cmd[@]}" >"${command_file}.tmp.$$" &&
    printf '\n' >>"${command_file}.tmp.$$" &&
    mv -- "${command_file}.tmp.$$" "${command_file}" ||
    record_failure "${id}: could not record command"

  log "running ${id}"
  local started_epoch
  local finished_epoch
  local duration_seconds
  started_epoch="$(date +%s)"
  (
    cd "${repo}" || exit 1
    "${cmd[@]}"
  ) >"${json}" 2>"${stderr_file}"
  local rc=$?
  finished_epoch="$(date +%s)"
  duration_seconds=$((finished_epoch - started_epoch))
  write_value "${exit_file}" "${rc}"
  log "${id} exited ${rc} after ${duration_seconds}s"
  local summary_rc=0
  summarize_test "${id}" "${selector}" "${rc}" "${duration_seconds}" || summary_rc=$?
  if [[ "${rc}" -ne 0 ]]; then
    return "${rc}"
  fi
  return "${summary_rc}"
}

if [[ "${tests_allowed}" == true ]]; then
  if run_test \
    "01-index-segment-discriminant" \
    "-" \
    "^TestScalarRTreeSegmentDiscriminant$" \
    "./index" \
    "-"; then
    :
  else
    record_failure "01-index-segment-discriminant did not satisfy the selected pass/no-skip contract"
    tests_allowed=false
  fi
fi

if [[ "${tests_allowed}" == true ]]; then
  if run_test \
    "02-candidate-rtree-native" \
    "lance_external_lib,lance_rtree_native" \
    "^TestRTreeScalarIndexGeometryQueriesAndLifecycle$" \
    "." \
    "${CANDIDATE_LIB}"; then
    :
  else
    record_failure "02-candidate-rtree-native did not satisfy the selected pass/no-skip contract"
    tests_allowed=false
  fi
fi

if [[ "${tests_allowed}" == true ]]; then
  if run_test \
    "03-legacy-rtree-rejection" \
    "lance_external_lib,lance_rtree_legacy" \
    "^TestRTreeRejectedByLegacyNative$" \
    "." \
    "${LEGACY_LIB}"; then
    :
  else
    record_failure "03-legacy-rtree-rejection did not satisfy the selected pass/no-skip contract"
    tests_allowed=false
  fi
fi

log "hashing consumed library/header inputs after execution"
if record_hashes "hashes-after.tsv"; then
  if validate_hashes_expected "${out}/hashes-after.tsv"; then
    hashes_after_expected=true
  else
    record_failure "consumed library/header hashes do not match retained identities after execution"
  fi
  if cmp -s \
    <(awk -F '\t' 'BEGIN {OFS="\t"} {print $1, $2, $3, $5}' "${out}/hashes-before.tsv") \
    <(awk -F '\t' 'BEGIN {OFS="\t"} {print $1, $2, $3, $5}' "${out}/hashes-after.tsv"); then
    hashes_after_same=true
  else
    diff -u \
      <(awk -F '\t' 'BEGIN {OFS="\t"} {print $1, $2, $3, $5}' "${out}/hashes-before.tsv") \
      <(awk -F '\t' 'BEGIN {OFS="\t"} {print $1, $2, $3, $5}' "${out}/hashes-after.tsv") \
      >"${out}/hashes.diff" || true
    record_failure "consumed library/header bytes changed during execution"
  fi
else
  record_failure "could not hash consumed library/header inputs after execution"
fi

if [[ -d "${repo}/.git" ]]; then
  "${GIT_BIN}" -C "${repo}" status --porcelain=v1 --untracked-files=all >"${out}/source-status-after.txt" 2>"${out}/source-status-after.stderr" || true
  if [[ ! -s "${out}/source-status-after.txt" ]]; then
    source_clean_after=true
  else
    record_failure "source checkout was not clean after execution"
  fi
  remote_ref_after="$("${GIT_BIN}" -C "${repo}" ls-remote origin "refs/heads/${CANDIDATE_BRANCH}" 2>/dev/null | awk 'NR == 1 {print $1}')"
else
  remote_ref_after=""
  : >"${out}/source-status-after.txt"
fi
write_value "${out}/remote-ref-after.txt" "${remote_ref_after}"

summary_files=()
for summary_file in \
  "${out}/01-index-segment-discriminant.summary.json" \
  "${out}/02-candidate-rtree-native.summary.json" \
  "${out}/03-legacy-rtree-rejection.summary.json"; do
  if [[ -f "${summary_file}" ]]; then
    summary_files+=("${summary_file}")
  fi
done

if [[ ${#summary_files[@]} -gt 0 ]]; then
  "${JQ_BIN}" -s '
    {
      processes: length,
      run_count: (map(.run_count) | add // 0),
      pass_count: (map(.pass_count) | add // 0),
      fail_count: (map(.fail_count) | add // 0),
      skip_count: (map(.skip_count) | add // 0),
      top_level_run_count: (map(.top_level_run_count) | add // 0),
      subtest_run_count: (map(.subtest_run_count) | add // 0),
      selected_tests_ran: all(.[]; .selected_test_ran),
      selected_tests_passed: all(.[]; .selected_test_passed),
      any_skip: any(.[]; .any_skip),
      any_fail: any(.[]; .any_fail),
      all_processes_exit_zero: all(.[]; .exit_code == 0),
      details: .
    }
  ' "${summary_files[@]}" >"${out}/test-totals.json.tmp.$$" &&
    mv -- "${out}/test-totals.json.tmp.$$" "${out}/test-totals.json" ||
    record_failure "could not aggregate test counts"
else
  "${JQ_BIN}" -n '
    {
      processes: 0,
      run_count: 0,
      pass_count: 0,
      fail_count: 0,
      skip_count: 0,
      top_level_run_count: 0,
      subtest_run_count: 0,
      selected_tests_ran: false,
      selected_tests_passed: false,
      any_skip: false,
      any_fail: false,
      all_processes_exit_zero: false,
      details: []
    }
  ' >"${out}/test-totals.json"
fi

if ! "${JQ_BIN}" -e '
  .processes == 3 and
  .selected_tests_ran and
  .selected_tests_passed and
  (.any_skip | not) and
  (.any_fail | not) and
  .all_processes_exit_zero
' "${out}/test-totals.json" >/dev/null; then
  record_failure "not all three selected tests ran, passed, and avoided skips"
fi
if [[ "${hashes_before_ok}" != true || "${hashes_after_same}" != true || "${hashes_after_expected}" != true ]]; then
  record_failure "library/header identity checks were not all satisfied"
fi
if [[ "${source_clean_after}" != true ]]; then
  record_failure "source cleanliness check was not satisfied"
fi

candidate_lib_size="$(awk -F '\t' '$1 == "candidate-library" {print $3}' "${out}/hashes-before.tsv" 2>/dev/null || true)"
candidate_lib_sha="$(awk -F '\t' '$1 == "candidate-library" {print $5}' "${out}/hashes-before.tsv" 2>/dev/null || true)"
candidate_header_size="$(awk -F '\t' '$1 == "candidate-header" {print $3}' "${out}/hashes-before.tsv" 2>/dev/null || true)"
candidate_header_sha="$(awk -F '\t' '$1 == "candidate-header" {print $5}' "${out}/hashes-before.tsv" 2>/dev/null || true)"
legacy_lib_size="$(awk -F '\t' '$1 == "legacy-library" {print $3}' "${out}/hashes-before.tsv" 2>/dev/null || true)"
legacy_lib_sha="$(awk -F '\t' '$1 == "legacy-library" {print $5}' "${out}/hashes-before.tsv" 2>/dev/null || true)"

"${JQ_BIN}" -n \
  --arg candidate_commit "${CANDIDATE_COMMIT}" \
  --arg candidate_tree "${CANDIDATE_TREE}" \
  --arg base_commit "${BASE_COMMIT}" \
  --arg candidate_branch "${CANDIDATE_BRANCH}" \
  --arg transport_commit "${transport_commit}" \
  --arg remote_ref_at_start "${remote_ref_at_start}" \
  --arg remote_ref_after "${remote_ref_after}" \
  --arg go_version "${go_version}" \
  --arg candidate_lib_path "${CANDIDATE_LIB}" \
  --arg candidate_lib_size "${candidate_lib_size}" \
  --arg candidate_lib_sha "${candidate_lib_sha}" \
  --arg candidate_header_path "${CANDIDATE_HEADER}" \
  --arg candidate_header_size "${candidate_header_size}" \
  --arg candidate_header_sha "${candidate_header_sha}" \
  --arg legacy_lib_path "${LEGACY_LIB}" \
  --arg legacy_lib_size "${legacy_lib_size}" \
  --arg legacy_lib_sha "${legacy_lib_sha}" \
  --arg first_failure "${first_failure}" \
  --argjson gofmt_clean "${gofmt_clean}" \
  --argjson source_clean_after "${source_clean_after}" \
  --argjson hashes_before_ok "${hashes_before_ok}" \
  --argjson hashes_after_same "${hashes_after_same}" \
  --argjson hashes_after_expected "${hashes_after_expected}" \
  --argjson overall_exit "${overall_exit}" \
  --slurpfile gofmt "${out}/gofmt-summary.json" \
  --slurpfile tests "${out}/test-totals.json" \
  '{
    task: "lance-go RTree bounded acceptance",
    host: "nier",
    source: {
      candidate_commit: $candidate_commit,
      candidate_tree: $candidate_tree,
      base_commit: $base_commit,
      candidate_branch: $candidate_branch,
      transport_commit: $transport_commit,
      remote_ref_at_start: $remote_ref_at_start,
      remote_ref_after: $remote_ref_after,
      source_clean_after: $source_clean_after
    },
    toolchain: {
      go_version: $go_version
    },
    libraries: {
      candidate_library: {
        path: $candidate_lib_path,
        size: (if $candidate_lib_size == "" then null else ($candidate_lib_size | tonumber) end),
        sha256: $candidate_lib_sha
      },
      candidate_header: {
        path: $candidate_header_path,
        size: (if $candidate_header_size == "" then null else ($candidate_header_size | tonumber) end),
        sha256: $candidate_header_sha
      },
      legacy_library: {
        path: $legacy_lib_path,
        size: (if $legacy_lib_size == "" then null else ($legacy_lib_size | tonumber) end),
        sha256: $legacy_lib_sha
      },
      before_matches_expected: $hashes_before_ok,
      after_matches_before: $hashes_after_same,
      after_matches_expected: $hashes_after_expected
    },
    gofmt: ($gofmt[0] + {clean: $gofmt_clean}),
    tests: $tests[0],
    first_failure: (if ($first_failure | length) == 0 then null else $first_failure end),
    overall_exit: $overall_exit,
    verdict: (if $overall_exit == 0 then "PASS" else "FAIL" end),
    scope: "Explicit candidate and legacy external libraries only; not default-distribution bundle coverage."
  }' >"${out}/RESULT.json.tmp.$$" &&
  mv -- "${out}/RESULT.json.tmp.$$" "${out}/RESULT.json" ||
  record_failure "could not write RESULT.json"

test_processes="$("${JQ_BIN}" -r '.processes' "${out}/test-totals.json")"
test_runs="$("${JQ_BIN}" -r '.run_count' "${out}/test-totals.json")"
test_passes="$("${JQ_BIN}" -r '.pass_count' "${out}/test-totals.json")"
test_skips="$("${JQ_BIN}" -r '.skip_count' "${out}/test-totals.json")"
test_subtests="$("${JQ_BIN}" -r '.subtest_run_count' "${out}/test-totals.json")"
verdict="FAIL"
if [[ "${overall_exit}" -eq 0 ]]; then
  verdict="PASS"
fi

{
  printf '# Lance Go RTree bounded acceptance\n\n'
  printf -- '- Verdict: **%s** (exit `%s`)\n' "${verdict}" "${overall_exit}"
  printf -- '- Host: `nier`\n'
  printf -- '- Candidate: `%s`, tree `%s`\n' "${CANDIDATE_COMMIT}" "${CANDIDATE_TREE}"
  printf -- '- Base: `%s`\n' "${BASE_COMMIT}"
  printf -- '- Transport commit: `%s`\n' "${transport_commit}"
  printf -- '- Go: `%s` at `%s`\n' "${go_version}" "${GO_BIN}"
  printf -- '- gofmt: exit `%s`, clean `%s`, diff bytes `%s`\n' "${gofmt_exit}" "${gofmt_clean}" "${gofmt_diff_bytes}"
  printf -- '- Tests: `%s/3` selected processes; `%s` run, `%s` pass, `%s` skip; `%s` subtests\n' "${test_processes}" "${test_runs}" "${test_passes}" "${test_skips}" "${test_subtests}"
  printf -- '- Candidate library: `%s` bytes, SHA-256 `%s`\n' "${candidate_lib_size}" "${candidate_lib_sha}"
  printf -- '- Candidate header: `%s` bytes, SHA-256 `%s`\n' "${candidate_header_size}" "${candidate_header_sha}"
  printf -- '- Legacy library: `%s` bytes, SHA-256 `%s`\n' "${legacy_lib_size}" "${legacy_lib_sha}"
  printf -- '- Library identities: before expected `%s`, after same `%s`, after expected `%s`\n' "${hashes_before_ok}" "${hashes_after_same}" "${hashes_after_expected}"
  printf -- '- Source clean after: `%s`\n' "${source_clean_after}"
  printf -- '- First failure: `%s`\n' "${first_failure:-none}"
  printf '\n## Tests\n\n'
  printf '```text\n'
  cat "${out}/commands.txt" 2>/dev/null || true
  printf '```\n\n'
  printf '## Scope\n\n'
  printf 'Explicit candidate and legacy external-library routes only. This result does not claim default-distribution bundle coverage.\n\n'
  printf 'Raw `-json`, stderr, exit files, hashes, source identity, Go environment, and checksums are retained beside this file.\n'
} >"${out}/RESULT.md.tmp.$$" &&
  mv -- "${out}/RESULT.md.tmp.$$" "${out}/RESULT.md" ||
  overall_exit=1

log "finished with exit ${overall_exit}; first failure=${first_failure:-none}"
sync

manifest_tmp="$(mktemp "${TMPDIR:-/tmp}/rtree-go-sha256sums.XXXXXX")"
manifest_ok=false
if (
  cd "${out}" || exit 1
  find . -maxdepth 1 -type f ! -name 'SHA256SUMS' ! -name 'overall-exit.txt' -printf '%f\n' | LC_ALL=C sort |
    while IFS= read -r file; do
      "${SHA256SUM_BIN}" -- "${file}"
    done >"${manifest_tmp}"
); then
  manifest_ok=true
else
  overall_exit=1
fi

if [[ "${manifest_ok}" == true ]] && write_value "${out}/overall-exit.txt" "${overall_exit}"; then
  (
    cd "${out}" || exit 1
    "${SHA256SUM_BIN}" -- overall-exit.txt
  ) >>"${manifest_tmp}" && mv -- "${manifest_tmp}" "${out}/SHA256SUMS" || overall_exit=1
else
  rm -f -- "${manifest_tmp}"
  write_value "${out}/overall-exit.txt" "${overall_exit}" || true
fi

exit "${overall_exit}"
