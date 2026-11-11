package ffi

// lanceNamespaceResult mirrors LanceNamespaceResult. The native side owns every
// pointer until lance_namespace_result_close is called.
type lanceNamespaceResult struct {
	ResultJSON            *byte
	ResultBytes           *byte
	ResultBytesLen        uint64
	HasNamespaceError     int32
	NamespaceErrorCode    uint32
	NamespaceErrorMessage *byte
}
