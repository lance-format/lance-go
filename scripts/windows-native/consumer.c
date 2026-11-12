/*
 * Focused Windows native consumer for the pinned lance-c C ABI.
 *
 * This program uses only the public C header and the DLL import library. It
 * creates a dataset from a caller-owned Arrow C Data stream, closes it,
 * reopens it, scans it, verifies that a cloned handle outlives its source, and
 * checks thread-local error ownership. It also verifies that the DLL was
 * loaded from the supplied runtime directory rather than from a build tree or
 * a developer PATH entry, and that no redistributable VC runtime module is
 * loaded before or after native operations. Only the exact OS components
 * msvcp_win.dll, msvcrt.dll, and ucrtbase.dll are accepted.
 */

#define _CRT_SECURE_NO_WARNINGS

#include <ctype.h>
#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <windows.h>
#include <tlhelp32.h>

#include "lance/lance.h"

static int failures;
static int batch_release_calls;
static int stream_release_calls;
static int schema_release_calls;
static int input_schema_release_calls;

typedef struct {
	struct ArrowSchema child;
	struct ArrowSchema* children[1];
} schema_state;

typedef struct {
	int64_t values[3];
	const void* buffers[2];
	const void* root_buffers[1];
	struct ArrowArray child;
	struct ArrowArray* children[1];
	struct ArrowArray array;
} batch_state;

typedef struct stream_state {
	struct ArrowArrayStream stream;
	int emitted;
	const char* last_error;
} stream_state;

static void fail(const char* what) {
	fprintf(stderr, "FAIL: %s\n", what);
	failures++;
}

static int require(int condition, const char* what) {
	if (!condition) {
		fail(what);
		return 0;
	}
	return 1;
}

static void release_leaf_schema(struct ArrowSchema* schema) {
	memset(schema, 0, sizeof(*schema));
}

static void dispose_schema(struct ArrowSchema* schema) {
	schema_state* state = (schema_state*)schema->private_data;
	if (state != NULL && state->child.release != NULL) {
		state->child.release(&state->child);
	}
	free(state);
	memset(schema, 0, sizeof(*schema));
}

static void release_schema(struct ArrowSchema* schema) {
	schema_release_calls++;
	dispose_schema(schema);
}

static void release_input_schema(struct ArrowSchema* schema) {
	input_schema_release_calls++;
	dispose_schema(schema);
}

static int fill_schema(struct ArrowSchema* out) {
	schema_state* state = (schema_state*)calloc(1, sizeof(*state));
	memset(out, 0, sizeof(*out));
	if (state == NULL) {
		return ENOMEM;
	}
	state->child.format = "l";
	state->child.name = "id";
	state->child.release = release_leaf_schema;
	state->children[0] = &state->child;
	out->format = "+s";
	out->n_children = 1;
	out->children = state->children;
	out->release = release_schema;
	out->private_data = state;
	return 0;
}

static void release_leaf_array(struct ArrowArray* array) {
	memset(array, 0, sizeof(*array));
}

static void release_batch(struct ArrowArray* array) {
	batch_state* batch = (batch_state*)array->private_data;
	batch_release_calls++;
	if (batch != NULL && batch->child.release != NULL) {
		batch->child.release(&batch->child);
	}
	memset(array, 0, sizeof(*array));
	free(batch);
}

static void release_stream(struct ArrowArrayStream* stream) {
	stream_state* state = (stream_state*)stream->private_data;
	stream_release_calls++;
	/* Returned batches own their buffers independently of the stream. */
	memset(stream, 0, sizeof(*stream));
	free(state);
}

static int stream_get_schema(struct ArrowArrayStream* stream, struct ArrowSchema* out) {
	stream_state* state = (stream_state*)stream->private_data;
	int rc = fill_schema(out);
	if (rc != 0) {
		state->last_error = "out of memory allocating Arrow schema";
	}
	return rc;
}

static int stream_get_next(struct ArrowArrayStream* stream, struct ArrowArray* out) {
	stream_state* state = (stream_state*)stream->private_data;
	batch_state* batch;

	if (state->emitted) {
		memset(out, 0, sizeof(*out));
		return 0;
	}

	batch = (batch_state*)calloc(1, sizeof(*batch));
	if (batch == NULL) {
		state->last_error = "out of memory allocating Arrow batch";
		memset(out, 0, sizeof(*out));
		return ENOMEM;
	}

	batch->values[0] = 1;
	batch->values[1] = 2;
	batch->values[2] = 3;
	batch->buffers[0] = NULL;
	batch->buffers[1] = batch->values;

	batch->child.length = 3;
	batch->child.n_buffers = 2;
	batch->child.buffers = batch->buffers;
	batch->child.release = release_leaf_array;
	batch->children[0] = &batch->child;
	batch->array.length = 3;
	batch->array.n_buffers = 1;
	batch->array.buffers = batch->root_buffers;
	batch->array.n_children = 1;
	batch->array.children = batch->children;
	batch->array.release = release_batch;
	batch->array.private_data = batch;

	state->emitted = 1;
	*out = batch->array;
	return 0;
}

static const char* stream_get_last_error(struct ArrowArrayStream* stream) {
	stream_state* state = (stream_state*)stream->private_data;
	return state != NULL ? state->last_error : "stream is closed";
}

static struct ArrowArrayStream make_stream(void) {
	stream_state* state = (stream_state*)calloc(1, sizeof(*state));
	struct ArrowArrayStream stream;

	if (state == NULL) {
		fprintf(stderr, "FAIL: out of memory allocating Arrow stream\n");
		exit(1);
	}
	memset(&stream, 0, sizeof(stream));
	state->stream.get_schema = stream_get_schema;
	state->stream.get_next = stream_get_next;
	state->stream.get_last_error = stream_get_last_error;
	state->stream.release = release_stream;
	state->stream.private_data = state;
	return state->stream;
}

static void print_native_error(const char* what) {
	LanceErrorCode code = lance_last_error_code();
	const char* message = lance_last_error_message();
	char copy[2048];

	copy[0] = '\0';
	if (message != NULL) {
		snprintf(copy, sizeof(copy), "%s", message);
		lance_free_string(message);
	}
	fprintf(stderr, "FAIL: %s: code=%d message=%s\n", what, (int)code,
		copy[0] != '\0' ? copy : "(null)");
	failures++;
}

static int check_native_ok(const char* what) {
	LanceErrorCode code = lance_last_error_code();
	if (code != LANCE_OK) {
		print_native_error(what);
		return 0;
	}
	return 1;
}

static int same_file(const char* left, const char* right) {
	HANDLE left_handle;
	HANDLE right_handle;
	BY_HANDLE_FILE_INFORMATION left_info;
	BY_HANDLE_FILE_INFORMATION right_info;
	int same = 0;

	memset(&left_info, 0, sizeof(left_info));
	memset(&right_info, 0, sizeof(right_info));

	left_handle = CreateFileA(left, FILE_READ_ATTRIBUTES,
		FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
		NULL, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, NULL);
	if (left_handle == INVALID_HANDLE_VALUE) {
		return 0;
	}
	right_handle = CreateFileA(right, FILE_READ_ATTRIBUTES,
		FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
		NULL, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, NULL);
	if (right_handle == INVALID_HANDLE_VALUE) {
		CloseHandle(left_handle);
		return 0;
	}
	if (GetFileInformationByHandle(left_handle, &left_info) &&
		GetFileInformationByHandle(right_handle, &right_info)) {
		same = left_info.dwVolumeSerialNumber == right_info.dwVolumeSerialNumber &&
			left_info.nFileIndexHigh == right_info.nFileIndexHigh &&
			left_info.nFileIndexLow == right_info.nFileIndexLow;
	}
	CloseHandle(right_handle);
	CloseHandle(left_handle);
	return same;
}

static void check_loaded_runtime(const char* runtime_dir) {
	HMODULE module;
	char loaded[4096];
	char expected[4096];
	DWORD length;
	size_t runtime_len;

	module = GetModuleHandleA("lance_c.dll");
	if (!require(module != NULL, "lance_c.dll is not loaded")) {
		return;
	}
	length = GetModuleFileNameA(module, loaded, (DWORD)sizeof(loaded));
	if (!require(length != 0 && length < (DWORD)sizeof(loaded), "GetModuleFileNameA failed")) {
		return;
	}

	runtime_len = strlen(runtime_dir);
	while (runtime_len > 0 &&
		(runtime_dir[runtime_len - 1] == '\\' || runtime_dir[runtime_len - 1] == '/')) {
		runtime_len--;
	}
	snprintf(expected, sizeof(expected), "%.*s\\lance_c.dll", (int)runtime_len, runtime_dir);
	printf("loaded_module=%s\n", loaded);
	if (!require(same_file(loaded, expected),
		"lance_c.dll was not loaded from the staged runtime directory")) {
		fprintf(stderr, "expected_module=%s\n", expected);
	}
}

static int has_prefix(const char* value, const char* prefix) {
	return strncmp(value, prefix, strlen(prefix)) == 0;
}

static int is_exact_os_crt_module(const char* name) {
	char lower[512];
	size_t i;

	for (i = 0; name[i] != '\0' && i + 1 < sizeof(lower); i++) {
		lower[i] = (char)tolower((unsigned char)name[i]);
	}
	lower[i] = '\0';
	return strcmp(lower, "msvcp_win.dll") == 0 ||
		strcmp(lower, "msvcrt.dll") == 0 ||
		strcmp(lower, "ucrtbase.dll") == 0;
}

static int is_redistributable_crt_module(const char* name) {
	char lower[512];
	size_t i;

	if (is_exact_os_crt_module(name)) {
		return 0;
	}
	for (i = 0; name[i] != '\0' && i + 1 < sizeof(lower); i++) {
		lower[i] = (char)tolower((unsigned char)name[i]);
	}
	lower[i] = '\0';
	return has_prefix(lower, "vcruntime") ||
		has_prefix(lower, "concrt") ||
		has_prefix(lower, "vcomp") ||
		has_prefix(lower, "vccorlib") ||
		has_prefix(lower, "mfc") ||
		has_prefix(lower, "msvcp") ||
		has_prefix(lower, "msvcr") ||
		has_prefix(lower, "ucrtbase");
}

static void check_crt_modules(const char* phase) {
	HANDLE snapshot;
	MODULEENTRY32A entry;
	char os_names[8][256];
	char joined[2048];
	int os_count = 0;
	int redist_count = 0;
	int index;

	snapshot = CreateToolhelp32Snapshot(TH32CS_SNAPMODULE | TH32CS_SNAPMODULE32,
		GetCurrentProcessId());
	if (snapshot == INVALID_HANDLE_VALUE) {
		fprintf(stderr, "FAIL: %s: CreateToolhelp32Snapshot failed: %lu\n",
			phase, (unsigned long)GetLastError());
		failures++;
		return;
	}

	memset(&entry, 0, sizeof(entry));
	entry.dwSize = (DWORD)sizeof(entry);
	if (Module32FirstA(snapshot, &entry)) {
		do {
			if (is_redistributable_crt_module(entry.szModule)) {
				fprintf(stderr,
					"FAIL: %s: redistributable CRT module loaded: %s (%s)\n",
					phase, entry.szModule, entry.szExePath);
				failures++;
				redist_count++;
			} else if (is_exact_os_crt_module(entry.szModule)) {
				int duplicate = 0;
				for (index = 0; index < os_count; index++) {
					if (_stricmp(os_names[index], entry.szModule) == 0) {
						duplicate = 1;
						break;
					}
				}
				if (!duplicate && os_count < (int)(sizeof(os_names) / sizeof(os_names[0]))) {
					snprintf(os_names[os_count], sizeof(os_names[0]), "%s", entry.szModule);
					os_count++;
				}
			}
		} while (Module32NextA(snapshot, &entry));
	} else {
		fprintf(stderr, "FAIL: %s: Module32FirstA failed: %lu\n",
			phase, (unsigned long)GetLastError());
		failures++;
	}
	CloseHandle(snapshot);

	joined[0] = '\0';
	for (index = 0; index < os_count; index++) {
		if (index > 0) {
			strncat(joined, ",", sizeof(joined) - strlen(joined) - 1);
		}
		strncat(joined, os_names[index], sizeof(joined) - strlen(joined) - 1);
	}
	printf("loaded_crt_modules_%s=%s\n", phase, os_count > 0 ? joined : "none");
	printf("redistributable_crt_modules_%s=%s\n", phase, redist_count > 0 ? "found" : "none");
	if (redist_count == 0) {
		printf("PASS: %s loaded CRT module guard\n", phase);
	}
}

static int check_rows(LanceDataset* dataset, uint64_t want, const char* what) {
	uint64_t rows = lance_dataset_count_rows(dataset);
	if (!check_native_ok(what)) {
		return 0;
	}
	if (!require(rows == want, what)) {
		fprintf(stderr, "got rows=%llu want=%llu\n",
			(unsigned long long)rows, (unsigned long long)want);
		return 0;
	}
	printf("PASS: %s rows=%llu\n", what, (unsigned long long)rows);
	return 1;
}

static int scan_dataset(LanceDataset* dataset, const char* what) {
	LanceScanner* scanner;
	struct ArrowArrayStream stream;
	struct ArrowSchema schema;
	int64_t scanned = 0;
	int64_t next_value = 1;
	int batches = 0;
	int failures_before = failures;
	int32_t rc;

	scanner = lance_scanner_new(dataset, NULL, NULL);
	if (!require(scanner != NULL, "lance_scanner_new returned NULL")) {
		print_native_error("lance_scanner_new");
		return 0;
	}

	memset(&stream, 0, sizeof(stream));
	rc = lance_scanner_to_arrow_stream(scanner, &stream);
	if (rc != 0) {
		print_native_error("lance_scanner_to_arrow_stream");
		lance_scanner_close(scanner);
		return 0;
	}

	memset(&schema, 0, sizeof(schema));
	rc = stream.get_schema(&stream, &schema);
	if (rc != 0) {
		fail("scan stream get_schema failed");
		if (stream.release != NULL) {
			stream.release(&stream);
		}
		lance_scanner_close(scanner);
		return 0;
	}
	require(schema.format != NULL && strcmp(schema.format, "+s") == 0 &&
		schema.n_children == 1 && schema.children != NULL &&
		schema.children[0] != NULL && schema.children[0]->format != NULL &&
		strcmp(schema.children[0]->format, "l") == 0,
		"scan schema is not a struct containing one int64 column");
	if (require(schema.release != NULL, "scan schema has no release callback")) {
		schema.release(&schema);
		require(schema.release == NULL, "scan schema release did not clear callback");
	}

	for (;;) {
		struct ArrowArray array;
		struct ArrowArray* column;
		memset(&array, 0, sizeof(array));
		rc = stream.get_next(&stream, &array);
		if (rc != 0) {
			const char* stream_error = stream.get_last_error != NULL
				? stream.get_last_error(&stream)
				: NULL;
			fprintf(stderr, "FAIL: scan stream get_next failed: %s\n",
				stream_error != NULL ? stream_error : "(no error)");
			failures++;
			break;
		}
		if (array.release == NULL) {
			break;
		}

		if (!require(array.length >= 0 && array.offset >= 0,
			"scan batch has a negative length or offset")) {
			array.release(&array);
			break;
		}
		if (!require(array.n_children == 1 && array.children != NULL && array.children[0] != NULL,
			"scan batch does not contain one column")) {
			array.release(&array);
			break;
		}
		column = array.children[0];
		if (!require(column->offset >= 0 && column->length >= array.offset + array.length &&
			column->n_buffers >= 2 && column->buffers != NULL && column->buffers[1] != NULL,
			"scan column has no valid int64 data buffer")) {
			array.release(&array);
			break;
		}
		if (!require(array.null_count == 0 && column->null_count == 0, "scan batch unexpectedly contains nulls")) {
			array.release(&array);
			break;
		}

		{
			const int64_t* values = (const int64_t*)column->buffers[1];
			int64_t row;
			for (row = 0; row < array.length; row++) {
				int64_t value = values[column->offset + array.offset + row];
				if (value != next_value) {
					fprintf(stderr,
						"FAIL: %s row %lld value=%lld want=%lld\n",
						what, (long long)(scanned + row),
						(long long)value, (long long)next_value);
					failures++;
					break;
				}
				next_value++;
			}
		}

		scanned += array.length;
		batches++;
		array.release(&array);
		require(array.release == NULL, "scan batch release did not clear callback");
	}

	if (require(stream.release != NULL, "scan stream has no release callback")) {
		stream.release(&stream);
		require(stream.release == NULL, "scan stream release did not clear callback");
	}
	lance_scanner_close(scanner);

	if (!require(scanned == 3, what)) {
		fprintf(stderr, "scanned rows=%lld\n", (long long)scanned);
		return 0;
	}
	if (!require(next_value == 4, "scan returned unexpected values")) {
		return 0;
	}
	if (!require(batches > 0, "scan stream returned no batches")) {
		return 0;
	}
	if (failures != failures_before) {
		return 0;
	}
	printf("PASS: %s scanned=%lld batches=%d values=1..3\n", what, (long long)scanned, batches);
	return 1;
}

int main(int argc, char** argv) {
	struct ArrowSchema schema;
	struct ArrowArrayStream stream;
	LanceDataset* created = NULL;
	LanceDataset* clone = NULL;
	LanceDataset* reopened = NULL;
	const char* uri;
	const char* runtime_dir;
	const char* missing_uri;
	uint64_t version;
	int32_t rc;

	if (argc != 3) {
		fprintf(stderr, "usage: %s <dataset-path> <runtime-dir>\n", argv[0]);
		return 2;
	}
	uri = argv[1];
	runtime_dir = argv[2];

	check_loaded_runtime(runtime_dir);
	if (failures != 0) {
		return 1;
	}
	check_crt_modules("before");
	if (failures != 0) {
		return 1;
	}

	if (!require(fill_schema(&schema) == 0, "cannot allocate input schema")) {
		return 1;
	}
	schema.release = release_input_schema;
	stream = make_stream();
	rc = lance_dataset_write(uri, &schema, &stream, LANCE_WRITE_CREATE, NULL, &created);
	if (schema.release != NULL) {
		schema.release(&schema);
	}
	if (rc != 0) {
		print_native_error("lance_dataset_write");
		return 1;
	}
	if (!require(created != NULL, "lance_dataset_write returned no dataset")) {
		return 1;
	}
	if (!require(input_schema_release_calls == 1, "caller schema release count is not 1")) {
		return 1;
	}
	if (!require(stream_release_calls == 1, "write stream release callback was not called exactly once")) {
		return 1;
	}
	if (!require(batch_release_calls == 1, "write stream batch release callback was not called exactly once")) {
		return 1;
	}
	if (!require(schema_release_calls == 1, "write stream schema release count is not 1")) {
		return 1;
	}
	printf("PASS: create\n");

	version = lance_dataset_version(created);
	if (!check_native_ok("lance_dataset_version") || !require(version == 1, "new dataset version is not 1")) {
		return 1;
	}
	printf("PASS: version=%llu\n", (unsigned long long)version);
	if (!check_rows(created, 3, "created dataset row count")) {
		return 1;
	}

	clone = lance_dataset_clone_handle(created);
	if (!require(clone != NULL, "lance_dataset_clone_handle returned NULL")) {
		print_native_error("lance_dataset_clone_handle");
		return 1;
	}
	lance_dataset_close(created);
	created = NULL;
	if (!check_rows(clone, 3, "cloned dataset outlives closed source")) {
		return 1;
	}
	lance_dataset_close(clone);
	clone = NULL;
	printf("PASS: clone lifetime\n");

	reopened = lance_dataset_open(uri, NULL, 0);
	if (!require(reopened != NULL, "lance_dataset_open after close returned NULL")) {
		print_native_error("lance_dataset_open");
		return 1;
	}
	if (!check_rows(reopened, 3, "reopened dataset row count")) {
		return 1;
	}
	if (!scan_dataset(reopened, "reopened dataset scan")) {
		return 1;
	}
	lance_dataset_close(reopened);
	reopened = NULL;
	printf("PASS: reopen and scan\n");

	missing_uri = argv[1];
	{
		size_t length = strlen(missing_uri);
		char* buffer = (char*)malloc(length + 9);
		if (buffer == NULL) {
			fprintf(stderr, "FAIL: out of memory building missing URI\n");
			return 1;
		}
		memcpy(buffer, missing_uri, length);
		memcpy(buffer + length, ".missing", 9);
		missing_uri = buffer;
	}
	created = lance_dataset_open(missing_uri, NULL, 0);
	if (!require(created == NULL, "opening a missing dataset unexpectedly succeeded")) {
		lance_dataset_close(created);
		return 1;
	}
	{
		LanceErrorCode code = lance_last_error_code();
		const char* message = lance_last_error_message();
		char copy[2048];

		if (!require(code != LANCE_OK, "missing dataset left LANCE_OK in thread-local storage")) {
			return 1;
		}
		if (!require(message != NULL && message[0] != '\0', "missing dataset produced no error message")) {
			return 1;
		}
		snprintf(copy, sizeof(copy), "%s", message);
		lance_free_string(message);
		if (!require(lance_last_error_code() == LANCE_OK,
			"reading the error message did not consume the native error")) {
			return 1;
		}
		printf("PASS: error ownership code=%d message=%s\n", (int)code, copy);
	}

	if (failures != 0) {
		fprintf(stderr, "FAIL: %d native consumer check(s) failed\n", failures);
		return 1;
	}
	check_crt_modules("after");
	if (failures != 0) {
		fprintf(stderr, "FAIL: %d native consumer check(s) failed\n", failures);
		return 1;
	}
	printf("PASS: windows native consumer\n");
	return 0;
}
