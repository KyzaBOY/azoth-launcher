package config

import "path/filepath"

// DataHomeEnv is the variable that holds the base of the data directory.
const DataHomeEnv = "LOCALAPPDATA"

// defaultDataHome is the base below the home folder when DataHomeEnv is not set.
var defaultDataHome = filepath.Join("AppData", "Local")
