// Copyright 2026 The mk Authors
// SPDX-License-Identifier: Apache-2.0

package mk

import "embed"

//go:embed std/*.mk
var stdlibFS embed.FS

//go:embed agents-guide.md
var AgentsGuide string
