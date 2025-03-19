// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

//go:build go1.5
// +build go1.5

package debug

import (
	"errors"
	"os"
	"path/filepath"
	"runtime/trace"
	"strings"

	srvflags "github.com/evmos/ethermint/server/flags"
	stderrors "github.com/pkg/errors"
)

func (a *API) startTrace(f *os.File) error {
	if err := trace.Start(f); err != nil {
		a.logger.Debug("Go tracing already started", "error", err.Error())
		if closeErr := f.Close(); closeErr != nil {
			a.logger.Debug("failed to close trace file", "error", closeErr.Error())
			return stderrors.Wrap(closeErr, "failed to close trace file")
		}
		return err
	}
	a.handler.traceFile = f
	return nil
}

// StartGoTrace turns on tracing, writing to the given file.
func (a *API) StartGoTrace(file string) error {
	a.logger.Debug("debug_startGoTrace", "file", file)
	a.handler.mu.Lock()
	defer a.handler.mu.Unlock()

	if a.handler.traceFile != nil {
		a.logger.Debug("trace already in progress")
		return errors.New("trace already in progress")
	}
	allowAny := a.ctx.Viper.GetBool(srvflags.JSONRPCAllowDebugTraceAnyFolder)
	if allowAny {
		fp, err := ExpandHome(file)
		if err != nil {
			a.logger.Debug("failed to get filepath for the CPU profile file", "error", err.Error())
			return err
		}
		f, err := os.Create(fp)
		if err != nil {
			a.logger.Debug("failed to create go trace file", "error", err.Error())
			return err
		}
		if err := a.startTrace(f); err != nil {
			return err
		}
		a.handler.traceFilename = file
		a.logger.Info("Go tracing started", "dump", a.handler.traceFilename)
		return nil
	}

	if file == ".." || file == "/" || file == "\\" || filepath.IsAbs(file) {
		a.logger.Debug("invalid file path provided", "file", file)
		return errors.New("invalid file path")
	}

	baseDir := a.ctx.Config.RootDir
	fullPath := filepath.Join(baseDir, file)

	dirPath := filepath.Dir(fullPath)
	canonicalDir, err := filepath.EvalSymlinks(dirPath)
	if err != nil {
		a.logger.Debug("failed to get filepath for the CPU profile file", "error", err.Error())
		return err
	}

	filename := filepath.Base(fullPath)
	canonicalPath := filepath.Join(canonicalDir, filename)
	cleanBase := filepath.Clean(baseDir)
	cleanCanonical := filepath.Clean(canonicalPath)

	relativePath, err := filepath.Rel(cleanBase, cleanCanonical)
	if err != nil || strings.HasPrefix(relativePath, "..") {
		a.logger.Debug("canonical path escapes base directory", "canonicalPath", canonicalPath, "baseDir", baseDir)
		return errors.New("path traversal attempt detected")
	}

	if err := os.MkdirAll(filepath.Dir(canonicalPath), 0o750); err != nil {
		a.logger.Debug("failed to create directory", "error", err.Error())
		return err
	}

	f, err := os.OpenFile(canonicalPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			a.logger.Debug("trace file already exists, refusing to overwrite", "file", canonicalPath)
			return errors.New("trace file already exists")
		}
		a.logger.Debug("failed to create go trace file", "error", err.Error())
		return err
	}
	if err := a.startTrace(f); err != nil {
		return err
	}
	a.handler.traceFilename = canonicalPath
	a.logger.Info("Go tracing started", "dump", a.handler.traceFilename)
	return nil
}

// StopGoTrace stops an ongoing trace.
func (a *API) StopGoTrace() error {
	a.logger.Debug("debug_stopGoTrace")
	a.handler.mu.Lock()
	defer a.handler.mu.Unlock()

	trace.Stop()
	if a.handler.traceFile == nil {
		a.logger.Debug("trace not in progress")
		return errors.New("trace not in progress")
	}
	a.logger.Info("Done writing Go trace", "dump", a.handler.traceFilename)
	if err := a.handler.traceFile.Close(); err != nil {
		a.logger.Debug("failed to close trace file")
		return stderrors.Wrap(err, "failed to close trace file")
	}
	a.handler.traceFile = nil
	a.handler.traceFilename = ""
	return nil
}
