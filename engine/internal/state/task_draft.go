package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kimNarr/Exord-init/engine/internal/id"
	"github.com/kimNarr/Exord-init/engine/internal/planner"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	taskcontract "github.com/kimNarr/Exord-init/engine/internal/task"
)

// OpenTaskDraftState anchors all draft I/O to the inspected project's ordinary
// .git/exord-init directory and takes the same OS lock as CREATE. Reject links
// in state paths rather than following them to another directory.
func OpenTaskDraftState(targetPath, lockID string) (*os.Root, *Lock, error) {
	target, err := os.OpenRoot(targetPath)
	if err != nil {
		return nil, nil, err
	}
	defer target.Close()
	git, err := target.Lstat(".git")
	if err != nil || !git.IsDir() || git.Mode()&os.ModeSymlink != 0 {
		return nil, nil, ErrUnsupportedGitFile
	}
	gitRoot, err := target.OpenRoot(".git")
	if err != nil {
		return nil, nil, err
	}
	defer gitRoot.Close()
	opened, err := gitRoot.Stat(".")
	if err != nil || !os.SameFile(git, opened) {
		return nil, nil, errors.New("Git directory changed")
	}
	if err := gitRoot.Mkdir("exord-init", 0700); err != nil && !os.IsExist(err) {
		return nil, nil, err
	}
	info, err := gitRoot.Lstat("exord-init")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("unsafe project state directory")
	}
	root, err := gitRoot.OpenRoot("exord-init")
	if err != nil {
		return nil, nil, err
	}
	opened, err = root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, nil, errors.New("project state directory changed")
	}
	if info, err := root.Lstat("lock.json"); err == nil && !info.Mode().IsRegular() {
		root.Close()
		return nil, nil, errors.New("unsafe lock file")
	} else if err != nil && !os.IsNotExist(err) {
		root.Close()
		return nil, nil, err
	}
	file, err := root.OpenFile("lock.json", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		root.Close()
		return nil, nil, err
	}
	if err := acquireOSLock(file.Fd()); err != nil {
		file.Close()
		root.Close()
		if errors.Is(err, errLockHeld) {
			return nil, nil, ErrLocked
		}
		return nil, nil, err
	}
	token, err := id.UUID4()
	if err == nil {
		err = writeLockPayload(file, lockState{SchemaVersion: 1, RunID: lockID, PID: os.Getpid(), OwnerToken: token, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}
	if err != nil {
		releaseOSLock(file.Fd())
		file.Close()
		root.Close()
		return nil, nil, err
	}
	return root, &Lock{file: file, ownerToken: token, root: root}, nil
}

func draftDirectory(projectID, branch string) string {
	sum := sha256.Sum256([]byte(projectID + "\x00" + branch))
	return "task-drafts/" + hex.EncodeToString(sum[:])
}

func draftRead(root *os.Root, path string, value any) error {
	info, err := root.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 512<<10 {
		return errors.New("unsafe draft record")
	}
	file, err := root.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (512<<10)+1))
	if err != nil || len(raw) > 512<<10 {
		return errors.New("draft record limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("trailing draft data")
	}
	return nil
}

func draftMkdir(root *os.Root, path string) error {
	current := ""
	for _, part := range strings.Split(path, "/") {
		if current != "" {
			current += "/"
		}
		current += part
		if err := root.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := root.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe draft directory")
		}
	}
	return nil
}

// Publish a complete synced record using a create-only link. A crash cannot
// expose half a JSON record or replace an earlier reviewed plan revision.
func draftPublish(root *os.Root, path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if len(raw) > 512<<10 {
		return errors.New("draft record limit exceeded")
	}
	token, err := id.UUID4()
	if err != nil {
		return err
	}
	slash := strings.LastIndexByte(path, '/')
	tempPath := path[:slash+1] + ".draft-tmp-" + token
	file, err := root.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(tempPath)
	defer file.Close()
	if _, err := file.Write(raw); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return root.Link(tempPath, path)
}

// ResolveTaskDraft must run under the project lock. The branch has one draft;
// changed intents preserve its identity and produce immutable plan revisions.
func ResolveTaskDraft(root *os.Root, projectID, identity, branch, selectedDraft string, used map[string]bool, now time.Time) (protocol.TaskDraft, error) {
	var draft protocol.TaskDraft
	if !isUUID(projectID) || len(identity) != 64 || !taskcontract.ValidBranch(branch) || (selectedDraft != "" && !isUUID(selectedDraft)) {
		return draft, errors.New("invalid draft binding")
	}
	if _, err := hex.DecodeString(identity); err != nil {
		return draft, errors.New("invalid target identity")
	}
	dir := draftDirectory(projectID, branch)
	if err := draftMkdir(root, "task-drafts"); err != nil {
		return draft, err
	}
	// Include identities of other local drafts in collision checks. Ignore only
	// unpublished temporary records, not unreadable or mis-bound identities.
	container, err := root.Open("task-drafts")
	if err != nil {
		return draft, err
	}
	entries, err := container.ReadDir(257)
	container.Close()
	if err != nil && err != io.EOF {
		return draft, err
	}
	if len(entries) > 256 {
		return draft, errors.New("local draft scan limit")
	}
	selected := false
	localIDs := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) != 64 {
			return draft, errors.New("unsafe local draft entry")
		}
		var other protocol.TaskDraft
		if err := draftRead(root, "task-drafts/"+entry.Name()+"/identity.json", &other); err != nil {
			return draft, errors.New("unreadable local draft identity")
		}
		if other.SchemaVersion != 1 || !isUUID(other.DraftID) || !taskcontract.ValidID(other.TaskID) || other.ProjectID != projectID || other.TargetIdentitySHA256 != identity || draftDirectory(other.ProjectID, other.WorkBranch) != "task-drafts/"+entry.Name() {
			return draft, errors.New("invalid local draft binding")
		}
		if _, err := time.Parse(time.RFC3339, other.CreatedAt); err != nil {
			return draft, errors.New("invalid local draft timestamp")
		}
		if localIDs[other.TaskID] {
			return draft, errors.New("duplicate local draft ID")
		}
		localIDs[other.TaskID] = true
		match := selectedDraft != "" && strings.EqualFold(other.DraftID, selectedDraft)
		if selectedDraft == "" {
			match = "task-drafts/"+entry.Name() == dir
		}
		if match {
			if selected || used[other.TaskID] {
				return draft, errors.New("existing draft ID collision")
			}
			draft = other
			selected = true
		}
	}
	if selected {
		return draft, nil
	}
	if selectedDraft != "" {
		return draft, errors.New("selected draft was not found")
	}
	for value := range localIDs {
		used[value] = true
	}
	if err := draftMkdir(root, dir); err != nil {
		return draft, err
	}
	taskID, err := taskcontract.NewUniqueID(now, used)
	if err != nil {
		return draft, err
	}
	draftID, err := id.UUID4()
	if err != nil {
		return draft, err
	}
	draft = protocol.TaskDraft{SchemaVersion: 1, DraftID: draftID, TaskID: taskID, CreatedAt: now.UTC().Truncate(time.Second).Format(time.RFC3339), ProjectID: projectID, TargetIdentitySHA256: identity, WorkBranch: branch}
	return draft, draftPublish(root, dir+"/identity.json", draft)
}

func PersistTaskPreparation(root *os.Root, value protocol.TaskPreparation) (protocol.TaskPreparation, error) {
	if err := planner.ValidateTaskPreparation(value); err != nil {
		return value, err
	}
	draft := value.Spec.Draft
	dir := draftDirectory(draft.ProjectID, draft.WorkBranch)
	var stored protocol.TaskDraft
	if err := draftRead(root, dir+"/identity.json", &stored); err != nil || stored != draft {
		return value, errors.New("stored task draft identity mismatch")
	}
	if err := draftMkdir(root, dir+"/plans"); err != nil {
		return value, err
	}
	path := dir + "/plans/" + value.SpecSHA256 + ".json"
	var previous protocol.TaskPreparation
	if err := draftRead(root, path, &previous); err == nil {
		if err := planner.ValidateTaskPreparation(previous); err != nil {
			return value, err
		}
		if previous.SpecSHA256 != value.SpecSHA256 || previous.Document != value.Document || previous.Spec.Draft != draft {
			return value, errors.New("stored plan revision mismatch")
		}
		return previous, nil
	} else if !os.IsNotExist(err) {
		return value, err
	}
	return value, draftPublish(root, path, value)
}
