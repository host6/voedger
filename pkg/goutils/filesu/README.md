# filesu

File system utilities for copying files and directories with configurable options.

## Problem

Provides a unified interface for copying files and directories from various file system sources to disk with flexible configuration options.

## Features

- **File copying** - Copy individual files with options
  - [File copy implementation: impl.go#L37](impl.go#L37)
  - [File copy from FS: impl.go#L29](impl.go#L29)
  - [File copy core logic: impl.go#L84](impl.go#L84)
- **Directory copying** - Recursively copy entire directories
  - [Directory copy implementation: impl.go#L45](impl.go#L45)
  - [Directory copy from FS: impl.go#L20](impl.go#L20)
  - [Recursive directory traversal: impl.go#L50](impl.go#L50)
- **File system abstraction** - Work with any fs.FS implementation
  - [IReadFS interface: types.go#L10](types.go#L10)
  - [FS integration: impl.go#L46](impl.go#L46)
- **Copy options** - Configurable copying behavior
  - [Options structure: types.go#L15](types.go#L15)
  - [File mode option: impl.go#L161](impl.go#L161)
  - [Skip existing option: impl.go#L167](impl.go#L167)
  - [File filtering option: impl.go#L179](impl.go#L179)
- **[File existence](impl.go#L151)** - Check if files or directories exist

## Use


