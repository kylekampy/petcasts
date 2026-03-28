package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Location struct {
	Name      string  `yaml:"name"`
	Latitude  float64 `yaml:"latitude"`
	Longitude float64 `yaml:"longitude"`
}

type Gemini struct {
	ImageModel string `yaml:"image_model"`
	ChatModel  string `yaml:"chat_model"`
}

type Display struct {
	Width  int `yaml:"width"`
	Height int `yaml:"height"`
}

type Cooldowns struct {
	PhotoDays int `yaml:"photo_days"`
	ComboDays int `yaml:"combo_days"`
	StyleUses int `yaml:"style_uses"`
}

type Pet struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Photos      []string `yaml:"photos"`
}

type Group struct {
	Name     string   `yaml:"name"`
	PetNames []string `yaml:"pets"`
}

type Config struct {
	Location  Location  `yaml:"location"`
	Styles    []string  `yaml:"styles"`
	Gemini    Gemini    `yaml:"gemini"`
	Display   Display   `yaml:"display"`
	Cooldowns Cooldowns `yaml:"cooldowns"`
	Pets      []Pet     `yaml:"-"`
	Groups    []Group   `yaml:"-"`
	DataDir   string    `yaml:"-"`
}

type petsFile struct {
	Pets   []Pet   `yaml:"pets"`
	Groups []Group `yaml:"groups"`
}

func Load(root string) (*Config, error) {
	data, err := os.ReadFile(filepath.Join(root, "config.yaml"))
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	petsData, err := os.ReadFile(filepath.Join(root, "pets", "meta", "pets.yaml"))
	if err != nil {
		return nil, err
	}
	var pf petsFile
	if err := yaml.Unmarshal(petsData, &pf); err != nil {
		return nil, err
	}

	cfg.Pets = pf.Pets
	cfg.Groups = pf.Groups
	cfg.DataDir = root
	return &cfg, nil
}

func (c *Config) PetByName(name string) *Pet {
	for i := range c.Pets {
		if c.Pets[i].Name == name {
			return &c.Pets[i]
		}
	}
	return nil
}

// PhotoToPets builds a reverse index from photo filename to the pets that reference it.
func (c *Config) PhotoToPets() map[string][]*Pet {
	m := make(map[string][]*Pet)
	for i := range c.Pets {
		for _, photo := range c.Pets[i].Photos {
			m[photo] = append(m[photo], &c.Pets[i])
		}
	}
	return m
}
