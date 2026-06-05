package pdo

import (
	"github.com/titpetric/pdo/client"
)

// Compile-time check that underlying client implements driver.
var _ driver = (*client.Client)(nil)
