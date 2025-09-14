package models


type DependencyChecker struct {
    Name    string   `yaml:"name"`
    Command string   `yaml:"command"`
    Args    []string `yaml:"args"`
    APIURL  string   `yaml:"api_url"`
    Enabled bool     `yaml:"enabled"`
}