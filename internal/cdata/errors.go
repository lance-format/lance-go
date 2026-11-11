package cdata

import (
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

var (
	// ErrInvalid reports malformed or inconsistent Arrow C data.
	ErrInvalid = errors.New("cdata: invalid Arrow C data")
	// ErrUnsupported reports an Arrow type that this package does not bridge.
	ErrUnsupported = errors.New("cdata: unsupported Arrow type")
)

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func unsupportedf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, fmt.Sprintf(format, args...))
}

const maxInt64Value = int64(1<<63 - 1)

func maxIntValue() int64 {
	return int64(^uint(0) >> 1)
}

func checkedAdd(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > maxInt64Value-b {
		return 0, invalidf("integer addition overflow: %d + %d", a, b)
	}
	return a + b, nil
}

func checkedMul(a, b int64) (int64, error) {
	if a < 0 || b < 0 || (a != 0 && b > maxInt64Value/a) {
		return 0, invalidf("integer multiplication overflow: %d * %d", a, b)
	}
	return a * b, nil
}

func checkedBytesForBits(bits int64) (int64, error) {
	bits, err := checkedAdd(bits, 7)
	if err != nil {
		return 0, err
	}
	return bits >> 3, nil
}

func toInt(v int64, what string) (int, error) {
	if v < 0 || v > maxIntValue() {
		return 0, invalidf("%s is outside the native int range: %d", what, v)
	}
	return int(v), nil
}

func isIntegerType(dt arrow.DataType) bool {
	if dt == nil {
		return false
	}
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64:
		return true
	default:
		return false
	}
}

func validateDataType(dt arrow.DataType) error {
	if dt == nil {
		return invalidf("nil Arrow data type")
	}
	switch t := dt.(type) {
	case *arrow.NullType,
		*arrow.BooleanType,
		*arrow.Int8Type, *arrow.Int16Type, *arrow.Int32Type, *arrow.Int64Type,
		*arrow.Uint8Type, *arrow.Uint16Type, *arrow.Uint32Type, *arrow.Uint64Type,
		*arrow.Float16Type, *arrow.Float32Type, *arrow.Float64Type,
		*arrow.Decimal32Type, *arrow.Decimal64Type, *arrow.Decimal128Type, *arrow.Decimal256Type,
		*arrow.Date32Type, *arrow.Date64Type,
		*arrow.Time32Type, *arrow.Time64Type, *arrow.TimestampType, *arrow.DurationType,
		*arrow.MonthIntervalType, *arrow.DayTimeIntervalType, *arrow.MonthDayNanoIntervalType,
		*arrow.BinaryType, *arrow.StringType, *arrow.LargeBinaryType, *arrow.LargeStringType,
		*arrow.ListType, *arrow.LargeListType, *arrow.StructType, *arrow.MapType:
		return nil
	case *arrow.FixedSizeBinaryType:
		if t.ByteWidth <= 0 {
			return invalidf("fixed-size binary byte width must be positive: %d", t.ByteWidth)
		}
		return nil
	case *arrow.FixedSizeListType:
		if t.Len() <= 0 {
			return invalidf("fixed-size list length must be positive: %d", t.Len())
		}
		return nil
	case *arrow.DictionaryType:
		if !isIntegerType(t.IndexType) {
			return unsupportedf("dictionary index type %s is not an integer", t.IndexType)
		}
		return validateDataType(t.ValueType)
	case arrow.ExtensionType:
		return unsupportedf("extension type %s", dt)
	default:
		return unsupportedf("%s (%s)", dt, dt.ID())
	}
}

func expectedBufferCount(dt arrow.DataType) (int, error) {
	switch dt.(type) {
	case *arrow.NullType:
		return 0, nil
	case *arrow.DictionaryType:
		return 2, nil
	case arrow.FixedWidthDataType:
		return 2, nil
	case *arrow.BinaryType, *arrow.StringType, *arrow.LargeBinaryType, *arrow.LargeStringType:
		return 3, nil
	case *arrow.ListType, *arrow.LargeListType, *arrow.MapType:
		return 2, nil
	case *arrow.FixedSizeListType, *arrow.StructType:
		return 1, nil
	default:
		return 0, unsupportedf("%s", dt)
	}
}

func expectedChildCount(dt arrow.DataType) (int, error) {
	switch t := dt.(type) {
	case *arrow.NullType:
		return 0, nil
	case *arrow.DictionaryType:
		return 0, nil
	case *arrow.StructType:
		return t.NumFields(), nil
	case *arrow.ListType, *arrow.LargeListType, *arrow.FixedSizeListType, *arrow.MapType:
		return 1, nil
	case arrow.FixedWidthDataType,
		*arrow.BinaryType, *arrow.StringType, *arrow.LargeBinaryType, *arrow.LargeStringType:
		return 0, nil
	default:
		return 0, unsupportedf("%s", dt)
	}
}
