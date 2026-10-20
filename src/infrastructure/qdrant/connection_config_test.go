package qdrant

// Test thuần (không cần Qdrant server) cho phần dựng cấu hình nén/on_disk/
// rescore — phần dễ sai âm thầm nhất vì Qdrant chỉ báo lỗi khi query thật.

import (
	"testing"

	qdrantapi "github.com/qdrant/go-client/qdrant"
)

func TestParseQuantization(t *testing.T) {
	cases := []struct {
		in   string
		want quantizationMode
	}{
		{"", quantNone},
		{"none", quantNone},
		{"scalar", quantScalar},
		{" SCALAR ", quantScalar},
		{"binary", quantBinary},
		{"Binary", quantBinary},
		{"int8", quantNone}, // giá trị lạ chỉ là lưới an toàn, config.Load đã chặn
	}
	for _, tc := range cases {
		if got := parseQuantization(tc.in); got != tc.want {
			t.Errorf("parseQuantization(%q) = %v, muốn %v", tc.in, got, tc.want)
		}
	}
}

func TestQuantizationModeString(t *testing.T) {
	if got := quantNone.String(); got != "none" {
		t.Errorf("quantNone.String() = %q, muốn none", got)
	}
	if got := quantScalar.String(); got == "none" || got == "" {
		t.Errorf("quantScalar.String() = %q, không được rỗng/none", got)
	}
	if got := quantBinary.String(); got != "binary" {
		t.Errorf("quantBinary.String() = %q, muốn binary", got)
	}
}

func TestQuantizationConfig(t *testing.T) {
	if cfg := (&connection{quant: quantNone}).quantizationConfig(); cfg != nil {
		t.Fatalf("quantNone phải trả nil, got %v", cfg)
	}

	scalar := (&connection{quant: quantScalar}).quantizationConfig()
	if scalar.GetScalar() == nil {
		t.Fatal("quantScalar phải dựng ScalarQuantization")
	}
	if got := scalar.GetScalar().GetType(); got != qdrantapi.QuantizationType_Int8 {
		t.Errorf("scalar type = %v, muốn Int8", got)
	}
	if got := scalar.GetScalar().GetQuantile(); got != 0.99 {
		t.Errorf("scalar quantile = %v, muốn 0.99", got)
	}
	if got := scalar.GetScalar().GetMemory(); got != qdrantapi.Memory_Pinned {
		t.Errorf("scalar memory = %v, muốn Pinned (vector nén nằm RAM)", got)
	}

	binary := (&connection{quant: quantBinary}).quantizationConfig()
	if binary.GetBinary() == nil {
		t.Fatal("quantBinary phải dựng BinaryQuantization")
	}
	if got := binary.GetBinary().GetMemory(); got != qdrantapi.Memory_Pinned {
		t.Errorf("binary memory = %v, muốn Pinned", got)
	}
}

func TestQuantizationDiff(t *testing.T) {
	if diff := (&connection{quant: quantNone}).quantizationDiff(); diff != nil {
		t.Fatalf("quantNone phải trả nil, got %v", diff)
	}
	if diff := (&connection{quant: quantScalar}).quantizationDiff(); diff.GetScalar() == nil {
		t.Fatal("quantScalar diff phải có Scalar")
	}
	if diff := (&connection{quant: quantBinary}).quantizationDiff(); diff.GetBinary() == nil {
		t.Fatal("quantBinary diff phải có Binary")
	}
}

func TestSearchParamsRescore(t *testing.T) {
	// Chưa nén: không gửi tham số rescore dù config bật.
	if p := (&connection{rescore: true, quantized: false, oversampling: 2}).searchParams(); p != nil {
		t.Fatalf("collection chưa nén phải trả nil, got %v", p)
	}
	// Đã nén nhưng config tắt rescore.
	if p := (&connection{rescore: false, quantized: true, oversampling: 2}).searchParams(); p != nil {
		t.Fatalf("rescore tắt phải trả nil, got %v", p)
	}
	p := (&connection{rescore: true, quantized: true, oversampling: 2.5}).searchParams()
	if !p.GetQuantization().GetRescore() {
		t.Error("rescore phải bật trong SearchParams")
	}
	if got := p.GetQuantization().GetOversampling(); got != 2.5 {
		t.Errorf("oversampling = %v, muốn 2.5", got)
	}
}

func TestUserIDIndexParams(t *testing.T) {
	if p := (&connection{principalIndex: false}).userIDIndexParams(); p != nil {
		t.Fatalf("principalIndex tắt phải trả nil, got %v", p)
	}
	p := (&connection{principalIndex: true}).userIDIndexParams()
	if !p.GetIntegerIndexParams().GetIsPrincipal() {
		t.Error("user_id phải có is_principal=true")
	}
}

func TestPayloadIndexSpecs(t *testing.T) {
	// Index user_id phải dùng params principal, các index số khác thì không —
	// lẫn params sang article_id sẽ partition dữ liệu theo bài, sai mục đích.
	specs := (&connection{principalIndex: true}).payloadIndexSpecs()
	byField := map[string]payloadIndexSpec{}
	for _, s := range specs {
		byField[s.field] = s
	}
	for _, field := range []string{"article_id", "user_id", "category_id", "chunk_index", "text"} {
		if _, ok := byField[field]; !ok {
			t.Errorf("thiếu index cho field %q", field)
		}
	}
	if byField["user_id"].params == nil {
		t.Error("user_id phải có params principal")
	}
	if byField["article_id"].params != nil {
		t.Error("article_id không được gắn params principal")
	}
	if byField["text"].typ != qdrantapi.FieldType_FieldTypeText {
		t.Errorf("text typ = %v, muốn FieldTypeText", byField["text"].typ)
	}
}
