package flyctl

import (
	"context"
	"testing"
	"time"

	"github.com/goombaio/namegenerator"
)

func TestFoundFlyctl(t *testing.T) {
	if flyctlPath == "" {
		t.Error("can't find flyctl")
	}
	t.Log(flyctlPath)
}

func TestWireGuardCreate(t *testing.T) {
	seed := time.Now().UTC().UnixNano()
	nameGenerator := namegenerator.NewNameGenerator(seed)
	name := nameGenerator.Generate()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	defer func() { WireGuardRemove(ctx, "personal", name) }()

	wgConfig, err := WireGuardCreate(ctx, "personal", "yyz", name)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(wgConfig)
}
