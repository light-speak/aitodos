package artifact

import "testing"

func TestCreateImageInputValidation(t *testing.T) {
	valid := CreateImageInput{
		OriginalName: " image.png ", OriginalMediaType: "image/png", Original: []byte("original"),
		OptimizedMediaType: "image/webp", Optimized: []byte("optimized"),
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []CreateImageInput{
		{},
		{OriginalName: "image.png"},
		{OriginalName: "image.png", OriginalMediaType: "image/png", Original: []byte("original")},
	}
	for _, input := range cases {
		if err := input.Validate(); err == nil {
			t.Fatalf("Validate(%#v) succeeded", input)
		}
	}
}
