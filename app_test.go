package main

import (
	"github.com/lainio/err2/try"
	"os"
	"testing"
)

func TestApp_Run(t *testing.T) {
	try.To(os.Setenv("LOG_LEVEL", "DEBUG"))
	try.To(os.Setenv("POLL_SINCE", "2400h"))
	try.To(os.Setenv("DOCKER_HOST", "tcp://localhost:2375"))
	try.To(os.Setenv("CONTAINER_ID", "47f77af81d29d68fa4c3871ef3b1dd0a3c17fcba18bca5dd51c8a467d49edbda"))
	app := try.To1(NewAppBuilder().Build())
	try.To(app.Run())
}
