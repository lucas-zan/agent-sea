// Package skill provides skill discovery and loading.
package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"AgentEngine/pkg/engine/api"

	"gopkg.in/yaml.v3"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SkillIndex Interface
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SkillIndex provides skill discovery and loading.
type SkillIndex interface {
	// List returns metadata for all indexed skills.
	List() []api.SkillMeta

	// Load returns the full skill content by name.
	Load(name string) (*api.Skill, error)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// DirSkillIndex
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

var (
	allowedFrontmatterKeys = map[string]struct{}{
		"name":          {},
		"description":   {},
		"license":       {},
		"compatibility": {},
		"metadata":      {},
		"allowed-tools": {},
	}

	skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// DirSkillIndex indexes skills by scanning one or more roots for SKILL.md files.
//
// Root ordering is significant: earlier roots take precedence when names collide.
type DirSkillIndex struct {
	roots []string
	index map[string]api.SkillMeta
	mu    sync.RWMutex
}

// NewDirSkillIndex creates a new directory-based skill index.
func NewDirSkillIndex(roots ...string) (*DirSkillIndex, error) {
	idx := &DirSkillIndex{
		roots: roots,
		index: make(map[string]api.SkillMeta),
	}
	if err := idx.Refresh(); err != nil {
		return nil, err
	}
	return idx, nil
}

// Refresh re-indexes all skill roots.
func (idx *DirSkillIndex) Refresh() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.index = make(map[string]api.SkillMeta)

	for _, root := range idx.roots {
		if root == "" {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if d.Name() != "SKILL.md" {
				return nil
			}

			meta, parseErr := parseSkillMeta(path)
			if parseErr != nil {
				return nil
			}

			if _, exists := idx.index[meta.Name]; !exists {
				idx.index[meta.Name] = meta
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to walk skill root %s: %w", root, err)
		}
	}

	return nil
}

// List returns all indexed skill metadata.
func (idx *DirSkillIndex) List() []api.SkillMeta {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	skills := make([]api.SkillMeta, 0, len(idx.index))
	for _, meta := range idx.index {
		skills = append(skills, meta)
	}
	return skills
}

// Load returns the full skill content.
func (idx *DirSkillIndex) Load(name string) (*api.Skill, error) {
	idx.mu.RLock()
	meta, exists := idx.index[name]
	idx.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("skill not found: %s", name)
	}

	skillFile := filepath.Join(meta.Path, "SKILL.md")
	raw, err := os.ReadFile(skillFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read SKILL.md: %w", err)
	}

	parsedMeta, body, fm, err := parseSkillMarkdown(skillFile, string(raw))
	if err != nil {
		return nil, err
	}

	// Ensure the loaded skill still matches the indexed name/path.
	if parsedMeta.Name != meta.Name {
		return nil, fmt.Errorf("skill name mismatch while loading: index=%q file=%q", meta.Name, parsedMeta.Name)
	}

	sk := &api.Skill{
		SkillMeta: api.SkillMeta{
			Name:          fm.Name,
			Description:   fm.Description,
			License:       fm.License,
			Compatibility: fm.Compatibility,
			AllowedTools:  append([]string(nil), fm.AllowedTools...),
			Path:          meta.Path,
		},
		Content:  strings.TrimSpace(body),
		Metadata: fm.Metadata,
	}

	// Optional directories for progressive disclosure.
	if entries, err := os.ReadDir(filepath.Join(meta.Path, "scripts")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			sk.Scripts = append(sk.Scripts, filepath.Join("scripts", e.Name()))
		}
	}
	if entries, err := os.ReadDir(filepath.Join(meta.Path, "references")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			sk.References = append(sk.References, filepath.Join("references", e.Name()))
		}
	}
	if entries, err := os.ReadDir(filepath.Join(meta.Path, "assets")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			sk.Assets = append(sk.Assets, filepath.Join("assets", e.Name()))
		}
	}

	return sk, nil
}

// Get returns skill metadata by name.
func (idx *DirSkillIndex) Get(name string) (api.SkillMeta, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	meta, ok := idx.index[name]
	return meta, ok
}

type parsedFrontmatter struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	Metadata      map[string]string
	AllowedTools  []string
}

func parseSkillMeta(skillFile string) (api.SkillMeta, error) {
	raw, err := os.ReadFile(skillFile)
	if err != nil {
		return api.SkillMeta{}, err
	}

	skillDir := filepath.Dir(skillFile)
	dirName := filepath.Base(skillDir)

	meta, _, _, err := parseSkillMarkdown(skillFile, string(raw))
	if err != nil {
		return api.SkillMeta{}, err
	}
	if meta.Name != dirName {
		return api.SkillMeta{}, fmt.Errorf("skill name %q doesn't match directory %q", meta.Name, dirName)
	}
	meta.Path = skillDir
	return meta, nil
}

func parseSkillMarkdown(skillFile string, raw string) (api.SkillMeta, string, parsedFrontmatter, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return api.SkillMeta{}, "", parsedFrontmatter{}, fmt.Errorf("invalid SKILL.md: missing YAML frontmatter (expected '---' on line 1): %s", skillFile)
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return api.SkillMeta{}, "", parsedFrontmatter{}, fmt.Errorf("invalid SKILL.md: missing closing YAML frontmatter delimiter '---': %s", skillFile)
	}

	frontmatterText := strings.Join(lines[1:end], "\n")
	bodyText := strings.Join(lines[end+1:], "\n")

	var rawFM map[string]any
	if err := yaml.Unmarshal([]byte(frontmatterText), &rawFM); err != nil {
		return api.SkillMeta{}, "", parsedFrontmatter{}, fmt.Errorf("failed to parse frontmatter: %w", err)
	}
	for k := range rawFM {
		if _, ok := allowedFrontmatterKeys[k]; !ok {
			return api.SkillMeta{}, "", parsedFrontmatter{}, fmt.Errorf("invalid frontmatter: unexpected key %q (strict Agent Skills spec)", k)
		}
	}

	fm, err := decodeFrontmatter(rawFM)
	if err != nil {
		return api.SkillMeta{}, "", parsedFrontmatter{}, err
	}
	if err := validateFrontmatter(fm); err != nil {
		return api.SkillMeta{}, "", parsedFrontmatter{}, err
	}

	meta := api.SkillMeta{
		Name:          fm.Name,
		Description:   fm.Description,
		License:       fm.License,
		Compatibility: fm.Compatibility,
		AllowedTools:  append([]string(nil), fm.AllowedTools...),
	}

	return meta, bodyText, fm, nil
}

func decodeFrontmatter(raw map[string]any) (parsedFrontmatter, error) {
	var fm parsedFrontmatter
	fm.Metadata = make(map[string]string)

	if v, ok := raw["name"]; ok {
		if s, ok := v.(string); ok {
			fm.Name = strings.TrimSpace(s)
		}
	}
	if v, ok := raw["description"]; ok {
		if s, ok := v.(string); ok {
			fm.Description = strings.TrimSpace(s)
		}
	}
	if v, ok := raw["license"]; ok {
		if s, ok := v.(string); ok {
			fm.License = strings.TrimSpace(s)
		}
	}
	if v, ok := raw["compatibility"]; ok {
		if s, ok := v.(string); ok {
			fm.Compatibility = strings.TrimSpace(s)
		}
	}

	if v, ok := raw["metadata"]; ok && v != nil {
		switch m := v.(type) {
		case map[string]any:
			for k, vv := range m {
				s, ok := vv.(string)
				if !ok {
					return parsedFrontmatter{}, fmt.Errorf("invalid frontmatter: metadata[%q] must be string", k)
				}
				fm.Metadata[k] = s
			}
		case map[any]any:
			for k, vv := range m {
				ks, ok := k.(string)
				if !ok {
					return parsedFrontmatter{}, fmt.Errorf("invalid frontmatter: metadata keys must be strings")
				}
				s, ok := vv.(string)
				if !ok {
					return parsedFrontmatter{}, fmt.Errorf("invalid frontmatter: metadata[%q] must be string", ks)
				}
				fm.Metadata[ks] = s
			}
		default:
			return parsedFrontmatter{}, fmt.Errorf("invalid frontmatter: metadata must be a mapping")
		}
	}

	if v, ok := raw["allowed-tools"]; ok && v != nil {
		switch vv := v.(type) {
		case []any:
			for _, it := range vv {
				s, ok := it.(string)
				if !ok {
					return parsedFrontmatter{}, fmt.Errorf("invalid frontmatter: allowed-tools entries must be strings")
				}
				s = strings.TrimSpace(s)
				if s != "" {
					fm.AllowedTools = append(fm.AllowedTools, s)
				}
			}
		case []string:
			for _, s := range vv {
				s = strings.TrimSpace(s)
				if s != "" {
					fm.AllowedTools = append(fm.AllowedTools, s)
				}
			}
		case string:
			fm.AllowedTools = append(fm.AllowedTools, strings.Fields(vv)...)
		default:
			return parsedFrontmatter{}, fmt.Errorf("invalid frontmatter: allowed-tools must be a list of strings")
		}
	}

	return fm, nil
}

func validateFrontmatter(fm parsedFrontmatter) error {
	if fm.Name == "" {
		return fmt.Errorf("invalid frontmatter: missing required field 'name'")
	}
	if len(fm.Name) > 64 {
		return fmt.Errorf("invalid frontmatter: 'name' must be <= 64 characters (got %d)", len(fm.Name))
	}
	if !skillNamePattern.MatchString(fm.Name) {
		return fmt.Errorf("invalid frontmatter: 'name' must be lowercase letters/numbers with single hyphens only (got %q)", fm.Name)
	}
	if fm.Description == "" {
		return fmt.Errorf("invalid frontmatter: missing required field 'description'")
	}
	return nil
}
