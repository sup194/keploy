package utgen

import (
	"go.keploy.io/server/v2/utils/log"
	"testing"
)

func TestUpdateJavaImports(t *testing.T) {
	logger, err := log.New()
	if err != nil {
		t.Fatal(err)
	}
	injector := NewInjectorBuilder()
}
