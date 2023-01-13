package main

import (
	"github.com/lainio/err2/try"
	"os"
	"testing"
)

func TestApp_Run(t *testing.T) {
	try.To(os.Setenv("LOG_LEVEL", "DEBUG"))
	try.To(os.Setenv("DOCKER_HOST", "tcp://localhost:2375"))
	try.To(os.Setenv("CONTAINER_ID", "ed647ada5dd22e3c9914156ae534d21ae5e73698717624b83c38dd714abf98b7"))
	app := try.To1(NewAppBuilder().Build())
	try.To(app.Run())
}
