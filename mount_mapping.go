package main

import (
	"path/filepath"
)

//go:generate gonstructor --type=MountMapping --constructorTypes=builder -init construct -propagateInitFuncReturns
type MountMapping struct {
	target string
	device string
}

func (x *MountMapping) construct() error {
	var err error
	if x.target, err = filepath.Abs(x.target); err != nil {
		return err
	}
	if x.device, err = filepath.Abs(x.device); err != nil {
		return err
	}
	return nil
}

func (x *MountMapping) translatePath(path string) (string, error) {
	deviceRelative, err := filepath.Rel(x.device, path)
	if err != nil {
		return "", err
	}
	return filepath.Join(x.target, deviceRelative), nil
}
