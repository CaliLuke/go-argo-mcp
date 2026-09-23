package mcpvalidation

import "testing"

func TestValidateExplicitLimitAcceptsNormalizedEmptyArguments(t *testing.T) {
	for _, arguments := range [][]byte{nil, {}, []byte(" \n\t "), []byte("{}"), []byte("null")} {
		if err := validateExplicitLimit(arguments); err != nil {
			t.Fatalf("empty arguments %q failed: %v", arguments, err)
		}
	}
}
