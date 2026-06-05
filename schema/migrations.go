package schema

import (
	_ "embed"
)

//go:embed users.up.sql
var Migrations string
