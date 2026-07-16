package mcpclient

import (
	"reflect"
	"testing"
)

func TestSetEnvironmentValueOverridesInheritedValue(t *testing.T) {
	environment := []string{"PATH=/bin", "TOKEN=old", "OTHER=value"}
	got := setEnvironmentValue(environment, "TOKEN", "new")
	want := []string{"PATH=/bin", "OTHER=value", "TOKEN=new"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}
