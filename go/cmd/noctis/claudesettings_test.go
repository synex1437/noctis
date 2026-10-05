package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// projectSettingsIn writes the project settings file name, settings.json or settings.local.json, of a session
// started in dir.
func projectSettingsIn(t *testing.T, dir, name string, settings any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if content, isText := settings.(string); isText {
		cliWrite(t, filepath.Join(dir, ".claude", name), []byte(content))
		return
	}
	mustWriteJSON(filepath.Join(dir, ".claude", name), settings)
}

// managedSettingsIn writes the file name, managed-settings.json or one of managed-settings.d, of the managed
// settings Claude Code reads.
func managedSettingsIn(t *testing.T, name string, settings any) {
	t.Helper()
	file := filepath.Join(files.managedSettings, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteJSON(file, settings)
}

// pointsIn are where Claude Code compacts a session started in dir on each of models, in a 1M window.
func pointsIn(t *testing.T, dir string, models ...string) []float64 {
	t.Helper()
	settings, points := compactionSettings(dir), []float64{}
	for _, model := range models {
		point, on := compactionPoint(settings, model, 1e6)
		if !on {
			t.Fatalf("compaction is off for %s", model)
		}
		points = append(points, point)
	}
	return points
}

func samePoints(got []float64, want ...float64) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func TestAProjectsSettingsAreLaidOverThePersonsAsClaudeCodeLaysThem(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	project := t.TempDir()
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000)})
	if got := pointsIn(t, project, "claude-opus-5-5"); !samePoints(got, 280000) {
		t.Fatalf("with no project settings the point is %v, want the person's 280000", got)
	}
	projectSettingsIn(t, project, "settings.json", object{"autoCompactWindow": float64(533000)})
	if got := pointsIn(t, project, "claude-opus-5-5"); !samePoints(got, 500000) {
		t.Fatalf("the project's autoCompactWindow 533000 gave %v, want 500000", got)
	}
	projectSettingsIn(t, project, "settings.local.json", object{"autoCompactWindow": float64(633000)})
	if got := pointsIn(t, project, "claude-opus-5-5"); !samePoints(got, 600000) {
		t.Fatalf("the local autoCompactWindow 633000 gave %v, want 600000", got)
	}
	if got := pointsIn(t, t.TempDir(), "claude-opus-5-5"); !samePoints(got, 280000) {
		t.Fatalf("a session in another directory took the project's window: %v", got)
	}
	// The percent override and the window variable of a project's env count as the person's do.
	projectSettingsIn(t, project, "settings.local.json", object{"env": object{autoCompactPercentVar: "40"}})
	if got := pointsIn(t, project, "claude-opus-5-5"); !samePoints(got, 205200) {
		t.Fatalf("the local percent 40 beside the project's window 533000 gave %v, want 205200", got)
	}
	if source := compactionSource(compactionSettings(project)); source != ".claude/settings.json: autoCompactWindow 533000, .claude/settings.local.json: CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=40" {
		t.Fatalf("the settings that decide are named as %q", source)
	}
	projectSettingsIn(t, project, "settings.json", object{"env": object{autoCompactWindowVar: "433000"}})
	if source := compactionSource(compactionSettings(project)); source != ".claude/settings.json: CLAUDE_CODE_AUTO_COMPACT_WINDOW=433000, .claude/settings.local.json: CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=40" {
		t.Fatalf("the window variable of the project is named as %q", source)
	}
}

func TestALaterWindowSetsAsideTheModelWindowsOfTheFilesBeforeIt(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	project := t.TempDir()
	models := []string{"claude-opus-5-5", "opus", "claude-sonnet-5-5", "claude-haiku-4-5"}
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000), "modelSettings": object{"claude-opus-5-5": object{"autoCompactWindow": float64(173000)}}})
	projectSettingsIn(t, project, "settings.json", object{"modelSettings": object{"sonnet": object{"autoCompactWindow": float64(233000)}}})
	if got := pointsIn(t, project, models...); !samePoints(got, 140000, 140000, 200000, 280000) {
		t.Fatalf("a project that gives Sonnet a window of its own, and no window for all, gave %v", got)
	}
	// Claude Code's gOt: a file that sets autoCompactWindow keeps only its own model windows.
	projectSettingsIn(t, project, "settings.json", object{"autoCompactWindow": float64(533000), "modelSettings": object{"sonnet": object{"autoCompactWindow": float64(233000)}}})
	if got := pointsIn(t, project, models...); !samePoints(got, 500000, 500000, 200000, 500000) {
		t.Fatalf("the project's window kept the person's window for Opus: %v", got)
	}
	// A later file's window for a model is over an earlier one's, under another name of the model.
	projectSettingsIn(t, project, "settings.local.json", object{"modelSettings": object{"opus": object{"autoCompactWindow": float64(433000)}, "claude-sonnet-5-5": object{"autoCompactWindow": "auto"}}})
	if got := pointsIn(t, project, models...); !samePoints(got, 400000, 400000, 967000, 500000) {
		t.Fatalf("the local model windows gave %v", got)
	}
	lines := strings.Join(compactWindowLinesIn(t, project), "\n")
	for _, want := range []string{"(.claude/settings.json: autoCompactWindow 533000)", "(.claude/settings.local.json: modelSettings.opus.autoCompactWindow 433000)", "(.claude/settings.local.json: modelSettings.claude-sonnet-5-5.autoCompactWindow auto)"} {
		if !strings.Contains(lines, want) {
			t.Errorf("status does not say %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, "173000") || strings.Contains(lines, "233000") {
		t.Errorf("status names a model window Claude Code sets aside:\n%s", lines)
	}
}

func TestInOneFileAModelsOwnNameIsTakenBeforeItsOtherNamesAndThenTheFirst(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	project := t.TempDir()
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000)})
	for _, file := range []struct {
		content string
		want    float64
	}{
		{`{"modelSettings": {"opus": {"autoCompactWindow": 233000}, "claude-opus-5-5[1m]": {"autoCompactWindow": 333000}}}`, 200000},
		{`{"modelSettings": {"claude-opus-5-5[1m]": {"autoCompactWindow": 333000}, "opus": {"autoCompactWindow": 233000}}}`, 300000},
		{`{"modelSettings": {"opus": {"autoCompactWindow": 233000}, "claude-opus-5-5": {"autoCompactWindow": 433000}}}`, 400000},
		{`{"modelSettings": {"opus": {"autoCompactWindow": 50000}, "claude-opus-5-5[1m]": {"autoCompactWindow": 333000}}}`, 300000},
	} {
		projectSettingsIn(t, project, "settings.json", file.content)
		if got := pointsIn(t, project, "claude-opus-5-5"); !samePoints(got, file.want) {
			t.Errorf("%s gave %v, want %v", file.content, got, file.want)
		}
	}
}

func TestManagedSettingsAndTheirDropInsAreLaidOverAllOthers(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	project := t.TempDir()
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000)})
	projectSettingsIn(t, project, "settings.local.json", object{"autoCompactWindow": float64(633000)})
	managedSettingsIn(t, "managed-settings.json", object{"autoCompactWindow": float64(433000), "modelSettings": object{"claude-opus-5-5": object{"effortLevel": "high"}}})
	if got := pointsIn(t, project, "claude-opus-5-5"); !samePoints(got, 400000) {
		t.Fatalf("managed-settings.json's window 433000 under a local 633000 gave %v, want 400000", got)
	}
	managedSettingsIn(t, "managed-settings.d/20-window.json", object{"autoCompactWindow": float64(383000)})
	managedSettingsIn(t, "managed-settings.d/10-window.json", object{"autoCompactWindow": float64(353000)})
	managedSettingsIn(t, "managed-settings.d/.hidden.json", object{"autoCompactWindow": float64(953000)})
	managedSettingsIn(t, "managed-settings.d/notes.txt", object{"autoCompactWindow": float64(953000)})
	if got := pointsIn(t, project, "claude-opus-5-5", "claude-sonnet-5-5"); !samePoints(got, 350000, 350000) {
		t.Fatalf("the drop-ins, the last by name over the others, gave %v, want 350000", got)
	}
	// A drop-in's window for a model is laid into the entry managed-settings.json keeps the model's effort in.
	managedSettingsIn(t, "managed-settings.d/30-opus.json", object{"modelSettings": object{"claude-opus-5-5": object{"autoCompactWindow": float64(173000)}}})
	if got := pointsIn(t, project, "claude-opus-5-5", "claude-sonnet-5-5"); !samePoints(got, 140000, 350000) {
		t.Fatalf("a drop-in's window for Opus gave %v, want 140000 for Opus and 350000 for Sonnet", got)
	}
	// Later drop-ins add to the models' entries, name by name and field by field, and take nothing away.
	managedSettingsIn(t, "managed-settings.d/35-sonnet.json", object{"modelSettings": object{"claude-sonnet-5-5": object{"autoCompactWindow": float64(233000)}}})
	managedSettingsIn(t, "managed-settings.d/36-effort.json", object{"modelSettings": object{"claude-opus-5-5": object{"effortLevel": "max"}}})
	if got := pointsIn(t, project, "claude-opus-5-5", "claude-sonnet-5-5"); !samePoints(got, 140000, 200000) {
		t.Fatalf("drop-ins for Sonnet's window and Opus's effort gave %v, want 140000 for Opus and 200000 for Sonnet", got)
	}
	managed := forwardSlashes(files.managedSettings)
	lines := strings.Join(compactWindowLinesIn(t, project), "\n")
	for _, want := range []string{"(" + managed + "/managed-settings.d/20-window.json: autoCompactWindow 383000)", "(" + managed + "/managed-settings.d/30-opus.json: modelSettings.claude-opus-5-5.autoCompactWindow 173000)"} {
		if !strings.Contains(lines, want) {
			t.Errorf("status does not say %q:\n%s", want, lines)
		}
	}
	managedSettingsIn(t, "managed-settings.d/40-off.json", object{"autoCompactEnabled": false})
	if why, off := autoCompactOff(compactionSettings(project)); !off || why != managed+"/managed-settings.d/40-off.json: autoCompactEnabled false" {
		t.Fatalf("a drop-in that turns compaction off: off %v, named as %q", off, why)
	}
}

func TestAProjectThatTurnsCompactionOffOrOnIsFollowed(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	project := t.TempDir()
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000)})
	projectSettingsIn(t, project, "settings.json", object{"autoCompactEnabled": false})
	if why, off := autoCompactOff(compactionSettings(project)); !off || why != ".claude/settings.json: autoCompactEnabled false" {
		t.Fatalf("the project's autoCompactEnabled false: off %v, named as %q", off, why)
	}
	if _, off := autoCompactOff(compactionSettings(t.TempDir())); off {
		t.Fatal("the project's switch turned compaction off in another directory")
	}
	projectSettingsIn(t, project, "settings.local.json", object{"autoCompactEnabled": true})
	if _, off := autoCompactOff(compactionSettings(project)); off {
		t.Fatal("the local autoCompactEnabled true did not turn compaction back on")
	}
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000), "autoCompactEnabled": false})
	projectSettingsIn(t, project, "settings.local.json", object{"autoCompactEnabled": "no"})
	if why, off := autoCompactOff(compactionSettings(project)); !off || why != ".claude/settings.json: autoCompactEnabled false" {
		t.Fatalf("a switch Claude Code does not take covered the one before it: off %v, named as %q", off, why)
	}
	projectSettingsIn(t, project, "settings.json", object{"env": object{"DISABLE_AUTO_COMPACT": "1"}})
	projectSettingsIn(t, project, "settings.local.json", object{})
	if why, off := autoCompactOff(compactionSettings(project)); !off || why != ".claude/settings.json: DISABLE_AUTO_COMPACT" {
		t.Fatalf("the project's DISABLE_AUTO_COMPACT: off %v, named as %q", off, why)
	}
}

func TestTheGuardWeighsTheContextByTheProjectsCompactionPoint(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	project := t.TempDir()
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000)})
	projectSettingsIn(t, project, "settings.json", object{"autoCompactWindow": float64(533000)})
	cfg := loadConfig()
	fill := contextFill{percent: 30, tokens: 300000, window: 1e6, known: true}
	if !compactionNear(cfg, "claude-opus-5-5", fill) {
		t.Fatal("300k tokens of a session that compacts at 280k are not near its compaction point")
	}
	fill.dir = project
	if compactionNear(cfg, "claude-opus-5-5", fill) {
		t.Fatal("300k tokens of a session whose project compacts at 500k count as near its compaction point")
	}
	if !compactionNear(cfg, "claude-opus-5-5", contextFill{percent: 48, tokens: 480000, window: 1e6, known: true, dir: project}) {
		t.Fatal("480k tokens of a session whose project compacts at 500k are not near its compaction point")
	}
	if session := sessionFill(object{"cwd": filepath.Join(project, "src"), "projectDir": project}); session.dir != project {
		t.Fatalf("a session's fill takes its directory as %q, want the project's", session.dir)
	}
	if session := sessionFill(object{"cwd": project}); session.dir != project {
		t.Fatalf("a session with no project directory takes its directory as %q, want its cwd", session.dir)
	}
}

func TestSettingsFilesThatCannotBeReadAreSetAsideAtOnce(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000)})
	notADirectory := t.TempDir()
	cliWrite(t, filepath.Join(notADirectory, ".claude"), []byte("a file where the directory would be\n"))
	aDirectory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(aDirectory, ".claude", "settings.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	broken, array := t.TempDir(), t.TempDir()
	projectSettingsIn(t, broken, "settings.json", `{"autoCompactWindow": 533000`)
	projectSettingsIn(t, array, "settings.local.json", `[{"autoCompactWindow": 533000}]`)
	if err := os.MkdirAll(filepath.Join(files.managedSettings, "managed-settings.d", "nested.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{notADirectory, aDirectory, broken, array, filepath.Join(t.TempDir(), "gone")} {
		start := time.Now()
		got := pointsIn(t, dir, "claude-opus-5-5")
		if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
			t.Errorf("%s took %v to set aside", dir, elapsed)
		}
		if !samePoints(got, 280000) {
			t.Errorf("%s gave %v, want the person's 280000", dir, got)
		}
	}
}

func TestASessionInTheHomeDirectoryReadsThePersonsSettingsOnce(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	home := t.TempDir()
	files.settings = filepath.Join(home, ".claude", "settings.json")
	projectSettingsIn(t, home, "settings.json", object{"autoCompactWindow": float64(533000)})
	if source := compactionSource(compactionSettings(home)); source != "autoCompactWindow 533000" {
		t.Fatalf("the person's settings.json, read as the home directory's project, is named as %q", source)
	}
}

func TestAnotherHostReadsOnlyItsSettings(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	previous := activeHost
	t.Cleanup(func() { activeHost = previous })
	activeHost = "copilot"
	project := t.TempDir()
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000)})
	projectSettingsIn(t, project, "settings.json", object{"autoCompactWindow": float64(533000)})
	managedSettingsIn(t, "managed-settings.json", object{"autoCompactWindow": float64(433000)})
	if got := pointsIn(t, project, "claude-opus-5-5"); !samePoints(got, 280000) {
		t.Fatalf("another host than Claude Code took Claude Code's project or managed settings: %v", got)
	}
}

func TestModelOverridesOfSeveralFilesAreGoneThroughInTheirMergedOrder(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	project := t.TempDir()
	cliWrite(t, files.settings, []byte(`{"modelOverrides": {"claude-opus-4-6": "team-model"}}`))
	projectSettingsIn(t, project, "settings.json", `{"modelOverrides": {"claude-opus-4-5": "team-model", "claude-sonnet-4-5": "other-model"}}`)
	naming := modelNamingOf(compactionSettings(project))
	if facts := layeredFactsOf(compactionSettings(project)); facts == nil || strings.Join(facts.overridesOrder, ",") != "claude-opus-4-6,claude-opus-4-5,claude-sonnet-4-5" {
		t.Fatalf("the overrides are gone through in the order %v", facts)
	}
	if got, _ := naming.overridden("team-model"); got != "claude-opus-4-6" {
		t.Fatalf("team-model maps back to %q, want the person's claude-opus-4-6, which comes first", got)
	}
}

func TestTheStatusLineAndTheNoticeFollowTheProjectTheSessionStartedIn(t *testing.T) {
	box := newLeanBox(t)
	box.settings(t, object{"autoCompactWindow": float64(313000)})
	project := t.TempDir()
	projectSettingsIn(t, project, "settings.json", object{"autoCompactWindow": float64(533000)})
	statusline := func(sid string, workspace object, cwd string, tokens float64) string {
		t.Helper()
		input := object{"session_id": sid, "model": object{"id": "claude-opus-5-5", "display_name": "Opus 5.5"}, "cwd": cwd, "context_window": object{"used_percentage": tokens / 1e4, "context_window_size": 1e6, "current_usage": object{"input_tokens": tokens}}}
		if workspace != nil {
			input["workspace"] = workspace
		}
		payload, _ := json.Marshal(input)
		run := box.run(t, string(payload), "statusline")
		if run.code != 0 {
			t.Fatalf("the status line failed:\n%s", run)
		}
		return run.stdout
	}
	inside := filepath.Join(project, "src")
	if line := statusline("s1", object{"current_dir": inside, "project_dir": project}, inside, 250000); !strings.Contains(line, "ctx 50%") {
		t.Fatalf("250k tokens in a project that compacts at 500k do not read 50%%: %q", line)
	}
	if line := statusline("s2", nil, project, 250000); !strings.Contains(line, "ctx 50%") {
		t.Fatalf("with no workspace the session's cwd does not name its project: %q", line)
	}
	if line := statusline("s3", nil, inside, 250000); !strings.Contains(line, "ctx 89%") {
		t.Fatalf("a session started below the project took its settings: %q", line)
	}
	statusline("s1", object{"current_dir": inside, "project_dir": project}, inside, 260000)
	if notice := box.prompt(t, "s1"); strings.Contains(notice, "/compact") {
		t.Fatalf("260k tokens in a project that compacts at 500k were told to compact: %q", notice)
	}
	statusline("s4", nil, box.home, 260000)
	if notice := box.prompt(t, "s4"); !strings.Contains(notice, "/compact") {
		t.Fatalf("260k tokens in a session that compacts at 280k got no notice: %q", notice)
	}
}

// compactWindowLinesIn are the status lines that say where Claude Code compacts, as noctis status gives them in
// dir.
func compactWindowLinesIn(t *testing.T, dir string) []string {
	t.Helper()
	t.Chdir(dir)
	return compactWindowStatusLines()
}

// realTempDir is a new directory with no link on its path, as the directory Claude Code takes a session's
// from the system is.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// linkedWorkTree makes the work tree tree a linked work tree (git worktree add) of the repository whose main
// work tree is main, as git lays one out: its .git file names main's .git/worktrees/name, whose commondir
// leads back to main's .git and whose gitdir points back at tree's .git.
func linkedWorkTree(t *testing.T, main, tree, name, back string) {
	t.Helper()
	gitDir := filepath.Join(main, ".git", "worktrees", name)
	mkdirs(t, gitDir, tree)
	cliWrite(t, filepath.Join(tree, ".git"), []byte("gitdir: "+gitDir+"\n"))
	cliWrite(t, filepath.Join(gitDir, "commondir"), []byte("../..\n"))
	cliWrite(t, filepath.Join(gitDir, "gitdir"), []byte(back+"\n"))
}

func TestASessionInAFolderOfAWorkTreeReadsTheLocalSettingsAtItsRoot(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	root := realTempDir(t)
	session := filepath.Join(root, "app", "web")
	mkdirs(t, filepath.Join(root, ".git"), session)
	projectSettingsIn(t, session, "settings.local.json", object{"autoCompactWindow": float64(533000), "modelSettings": object{"opus": object{"autoCompactWindow": float64(233000)}}})
	projectSettingsIn(t, root, "settings.local.json", object{"autoCompactWindow": float64(313000)})
	got, source := pointsIn(t, session, "claude-opus-5-5", "claude-sonnet-5-5"), compactionSource(compactionSettings(session))
	if isWindows {
		// Claude Code does not tell who owns a directory on Windows, and keeps a project's local settings in
		// the directory the session started in.
		if !samePoints(got, 200000, 500000) || source != ".claude/settings.local.json: autoCompactWindow 533000" {
			t.Fatalf("on Windows a session in a folder of a work tree gave %v, named %q", got, source)
		}
		return
	}
	// The root's settings.local.json is laid over the session directory's as one file: its window is over the
	// other's, and the other's model window stays.
	if !samePoints(got, 200000, 280000) {
		t.Fatalf("a session in a folder of a work tree gave %v, want 200000 for Opus and the root's 280000", got)
	}
	rootFile := forwardSlashes(filepath.Join(root, ".claude", "settings.local.json"))
	if source != rootFile+": autoCompactWindow 313000" {
		t.Fatalf("the root's window is named as %q", source)
	}
	lines := strings.Join(compactWindowLinesIn(t, session), "\n")
	if !strings.Contains(lines, "(.claude/settings.local.json: modelSettings.opus.autoCompactWindow 233000)") || !strings.Contains(lines, "("+rootFile+": autoCompactWindow 313000)") {
		t.Errorf("status does not name each setting's file:\n%s", lines)
	}
	// With no settings.local.json in the session directory, the root's is read alone.
	if err := os.Remove(filepath.Join(session, ".claude", "settings.local.json")); err != nil {
		t.Fatal(err)
	}
	if got := pointsIn(t, session, "claude-opus-5-5"); !samePoints(got, 280000) {
		t.Fatalf("the root's settings.local.json alone gave %v, want 280000", got)
	}
	// A session at the root reads the root's file once.
	if source := compactionSource(compactionSettings(root)); source != ".claude/settings.local.json: autoCompactWindow 313000" {
		t.Fatalf("a session at the root names its window as %q", source)
	}
}

func TestALinkedWorkTreeReadsTheLocalSettingsOfItsMainWorkTree(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	main, tree := realTempDir(t), filepath.Join(realTempDir(t), "feature")
	linkedWorkTree(t, main, tree, "feature", filepath.Join(tree, ".git"))
	projectSettingsIn(t, main, "settings.local.json", object{"autoCompactWindow": float64(313000)})
	session := filepath.Join(tree, "src")
	mkdirs(t, session)
	want := 280000.0
	if isWindows {
		want = 967000
	}
	for _, dir := range []string{tree, session} {
		if got := pointsIn(t, dir, "claude-opus-5-5"); !samePoints(got, want) {
			t.Errorf("a session in %s gave %v, want %v", dir, got, want)
		}
	}
	// A work tree whose git directory does not point back at it is not taken for a linked one, nor is one
	// whose git directory is not in the repository's worktrees: each keeps its own local settings.
	elsewhere := filepath.Join(realTempDir(t), ".git")
	mkdirs(t, elsewhere)
	linkedWorkTree(t, main, tree, "feature", elsewhere)
	if got := pointsIn(t, tree, "claude-opus-5-5"); !samePoints(got, 967000) {
		t.Errorf("a work tree its git directory does not point back at took the main work tree's settings: %v", got)
	}
	other := realTempDir(t)
	mkdirs(t, filepath.Join(other, ".git"))
	projectSettingsIn(t, other, "settings.local.json", object{"autoCompactWindow": float64(433000)})
	linkedWorkTree(t, main, tree, "feature", filepath.Join(tree, ".git"))
	cliWrite(t, filepath.Join(main, ".git", "worktrees", "feature", "commondir"), []byte(filepath.Join(other, ".git")+"\n"))
	if got := pointsIn(t, tree, "claude-opus-5-5"); !samePoints(got, 967000) {
		t.Errorf("a git directory outside the worktrees of the repository its commondir names led to that repository's settings: %v", got)
	}
}

func TestAWorkTreeAtTheHomeDirectoryKeepsTheLocalSettingsWhereTheSessionStarted(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	home := realTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	session := filepath.Join(home, "notes")
	mkdirs(t, filepath.Join(home, ".git"), session)
	projectSettingsIn(t, home, "settings.local.json", object{"autoCompactWindow": float64(313000)})
	if got := pointsIn(t, session, "claude-opus-5-5"); !samePoints(got, 967000) {
		t.Fatalf("a session in a folder of the home directory, a work tree, took its settings.local.json: %v", got)
	}
}
