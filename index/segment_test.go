package index

import "testing"

func TestScalarRTreeSegmentDiscriminant(t *testing.T) {
	if ScalarRTree != 9 {
		t.Fatalf("ScalarRTree = %d, want native value 9", ScalarRTree)
	}

	kind := SegmentIndexType(ScalarRTree)
	scalar, ok := kind.Scalar()
	if !ok || scalar != ScalarRTree {
		t.Fatalf("SegmentIndexType(%d).Scalar() = %d, %v; want %d, true", kind, scalar, ok, ScalarRTree)
	}
	if _, ok := SegmentIndexType(ScalarRTree + 1).Scalar(); ok {
		t.Fatal("scalar range accepted the first value after RTree")
	}
	if _, ok := kind.Vector(); ok {
		t.Fatal("RTree discriminant was also reported as a vector index")
	}
	if got := kind.String(); got != "RTREE" {
		t.Fatalf("SegmentIndexType(%d).String() = %q, want RTREE", kind, got)
	}
}
