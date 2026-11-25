package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/norma/skill"
)

// Every skill shipped in the repository's skills/ directory must parse cleanly
// with the real frontmatter parser and carry a description: a broken YAML
// scalar silently empties the metadata and the skill stops being discoverable
// by the agents (see the skills-YAML fix for the same failure mode).
func TestShippedSkillsParseCleanly(t *testing.T) {
	dir := filepath.Join("..", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no shipped skills dir: %v", err)
	}
	reg, err := skill.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir on shipped skills: %v", err)
	}
	byDir := map[string]skill.Skill{}
	if reg != nil {
		for _, sk := range reg.List() {
			if sk.Dir != "" {
				byDir[filepath.Base(sk.Dir)] = sk
			}
		}
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sk, ok := byDir[e.Name()]
		if !ok {
			t.Errorf("skill %s: not parsed by LoadDir (frontmatter broken?)", e.Name())
			continue
		}
		if sk.Description == "" {
			t.Errorf("skill %s: empty description after parse", e.Name())
		}
	}
	if _, ok := byDir["codeaudit"]; !ok {
		t.Error("codeaudit skill missing from shipped skills")
	}
}

// The Solana skills must keep the money rules the prompts state: simulation and
// local forks before anything live, mainnet only with an explicit approval, no
// secret key in the graph, and the operator-funded wallet flow through
// ask_operator. A later edit that drops one of them silently changes how the
// agents treat funds.
func TestSolanaSkillsKeepTheMoneyRules(t *testing.T) {
	skills, err := skill.LoadDir(filepath.Join("..", "skills"))
	if err != nil {
		t.Fatalf("LoadDir on shipped skills: %v", err)
	}
	byName := map[string]skill.Skill{}
	for _, sk := range skills.List() {
		byName[sk.Name] = sk
	}
	for _, name := range []string{"solana-recon", "anchor-audit", "solana-bytecode", "solana-exploit"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("Solana skill %s missing from shipped skills", name)
		}
	}
	for _, name := range []string{"api-recon", "api-security", "bbot-recon", "edr-bypass-re", "php2shell", "playwright-cli", "scopesentry"} {
		if _, ok := byName[name]; ok {
			t.Errorf("web-only skill %s still shipped", name)
		}
	}
	raw, err := os.ReadFile(filepath.Join("..", "skills", "solana-exploit", "SKILL.md"))
	if err != nil {
		t.Fatalf("read solana-exploit skill: %v", err)
	}
	text := strings.Join(strings.Fields(string(raw)), " ")
	for _, marker := range []string{
		"no state change, no fee, no approval needed",
		"mainnet-beta needs an explicit operator approval of that exact transaction",
		"Never print the key",
		"save_task_credential",
		"ask_operator",
		"For the proof of a missing signer, sign for real",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("solana-exploit skill missing marker %q", marker)
		}
	}
}
