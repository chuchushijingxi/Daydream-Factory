package modelconfig

import (
	"testing"
)

func TestModelConfigurationURLBoundaries(t *testing.T) {
	for _, url := range []string{"https://api.deepseek.com", "https://api.deepseek.com/v1", "http://127.0.0.1:1234", "http://[::1]:1234/v1"} {
		if e := ValidateModelURL(url); e != nil {
			t.Fatal(url, e)
		}
	}
	for _, url := range []string{"", "api.deepseek.com", "http://public.example", "https://key:secret@api.deepseek.com", "https://api.deepseek.com?api_key=dummy", "https://api.deepseek.com#dummy", " https://api.deepseek.com"} {
		if e := ValidateModelURL(url); e == nil {
			t.Fatal("invalid URL accepted", url)
		}
	}
}
