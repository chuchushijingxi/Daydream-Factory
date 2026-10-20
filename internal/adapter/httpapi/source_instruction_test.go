package httpapi_test

import (
	"os"
	"testing"
)

func sourceInstruction(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../application/project/testdata/contracts/source-instruction.txt")
	if err != nil {
		t.Fatal(err)
	}
	return "\n\n" + string(raw)
}
