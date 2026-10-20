// Package mailmigration handles the explicit, one-time private YAML transition.
// Runtime configuration never calls it or reads the obsolete mail section.
package mailmigration

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	"drama-agent/internal/adapter/appconfig"
	app "drama-agent/internal/application/mailconfig"
	"gopkg.in/yaml.v3"
)

type Document struct {
	Config            appconfig.Config
	Legacy            *app.Delivery
	path              string
	original, updated []byte
}

func Read(path string) (Document, error) {
	var out Document
	file, err := os.Open(path)
	if err != nil {
		return out, errors.New("cannot open private migration configuration")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1<<20 {
		return out, errors.New("migration input must be a private regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return out, errors.New("cannot read migration input")
	}
	var doc yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(&doc) != nil || decoder.Decode(new(any)) != io.EOF || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return out, errors.New("invalid migration YAML")
	}
	mapping := doc.Content[0]
	found := -1
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "mail" {
			if found >= 0 {
				return out, errors.New("duplicate legacy mail section")
			}
			found = i
		}
	}
	if found >= 0 {
		node := mapping.Content[found+1]
		encoded, e := yaml.Marshal(node)
		if e != nil {
			return out, errors.New("invalid legacy mail section")
		}
		var legacy struct {
			Address  string `yaml:"address"`
			Username string `yaml:"username"`
			Password string `yaml:"password"`
			From     string `yaml:"from"`
			TLS      string `yaml:"tls"`
		}
		decoder := yaml.NewDecoder(bytes.NewReader(encoded))
		decoder.KnownFields(true)
		if decoder.Decode(&legacy) != nil {
			return out, errors.New("invalid legacy mail fields")
		}
		settings, e := app.Validate(app.Settings{Enabled: true, Address: legacy.Address, Username: legacy.Username, From: legacy.From, TLS: legacy.TLS}, legacy.Password != "")
		if e != nil {
			return out, errors.New("invalid legacy SMTP configuration")
		}
		out.Legacy = &app.Delivery{Settings: settings, Password: legacy.Password}
		mapping.Content = append(mapping.Content[:found], mapping.Content[found+2:]...)
	}
	updated, err := yaml.Marshal(&doc)
	if err != nil {
		return out, errors.New("cannot prepare migrated YAML")
	}
	out.Config, err = appconfig.DecodeDocument(updated, path)
	if err != nil {
		return out, err
	}
	out.path = path
	out.original = raw
	out.updated = updated
	return out, nil
}

// Finalize is called only after PG commit and successful decrypt/readback. The
// original file survives every earlier failure; concurrent local edits are kept.
func (d Document) Finalize() error {
	if d.Legacy == nil {
		return nil
	}
	current, err := os.ReadFile(d.path)
	if err != nil || !bytes.Equal(current, d.original) {
		return errors.New("configuration changed during migration; original mail section retained")
	}
	file, err := os.CreateTemp(filepath.Dir(d.path), ".smtp-migration-*.local.yaml")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(d.updated); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, d.path); err != nil {
		return err
	}
	return nil
}
