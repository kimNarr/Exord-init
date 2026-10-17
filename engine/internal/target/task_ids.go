package target

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	taskcontract "github.com/kimNarr/Exord-init/engine/internal/task"
)

type boundedGitOutput struct {
	buffer  bytes.Buffer
	limited bool
}

func (out *boundedGitOutput) Len() int      { return out.buffer.Len() }
func (out *boundedGitOutput) Bytes() []byte { return out.buffer.Bytes() }

func (out *boundedGitOutput) Write(data []byte) (int, error) {
	const max = 8 << 20
	if len(data) > max-out.Len() {
		n, _ := out.buffer.Write(data[:max-out.Len()])
		out.limited = true
		return n, errors.New("Git output limit")
	}
	return out.buffer.Write(data)
}

func collisionGit(targetPath string, args ...string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", "-C", targetPath}, args...)...)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var out boundedGitOutput
	command.Stdout = &out
	command.Stderr = io.Discard
	err := command.Run()
	if out.limited {
		return out.Bytes(), true, nil
	}
	if err != nil {
		return nil, false, errors.New("cannot inspect local Task ID history")
	}
	return out.Bytes(), false, nil
}

// InspectTaskIDs inspects only locally available refs, bounded Task history and
// current worktree Task files. It never fetches and cannot promise remote-wide
// uniqueness. Limits are returned as explicit review evidence.
func InspectTaskIDs(path, currentBranch string) (map[string]bool, protocol.TaskCollisionScan, error) {
	used := map[string]bool{}
	scan := protocol.TaskCollisionScan{Limits: []string{}}
	branches, limited, err := collisionGit(path, "for-each-ref", "--count=257", "--format=%(refname)", "refs/heads/")
	if err != nil {
		return nil, scan, err
	}
	for _, branch := range bytes.Split(branches, []byte{'\n'}) {
		if string(branch) != "refs/heads/"+currentBranch {
			taskcontract.CollectIDs(branch, used)
		}
	}
	scan.Branches = bytes.Count(branches, []byte{'\n'})
	if limited || scan.Branches > 256 {
		scan.Limits = append(scan.Limits, "LOCAL_BRANCH_LIMIT")
	}
	messages, messageLimited, err := collisionGit(path, "log", "--all", "--max-count=257", "--format=EXORD_COMMIT_%H%n%B")
	if err != nil {
		return nil, scan, err
	}
	taskcontract.CollectIDs(messages, used)
	messageCount := bytes.Count(messages, []byte("EXORD_COMMIT_"))
	if messageLimited || messageCount > 256 {
		scan.Limits = appendUnique(scan.Limits, "LOCAL_HISTORY_LIMIT")
	}
	history, limited, err := collisionGit(path, "log", "--all", "--max-count=257", "--format=EXORD_COMMIT_%H%n%B", "--patch", "--no-ext-diff", "--no-textconv", "--no-renames", "--", "TASK.md", ".exord/TASK.md")
	if err != nil {
		return nil, scan, err
	}
	taskcontract.CollectIDs(history, used)
	patchCount := bytes.Count(history, []byte("EXORD_COMMIT_"))
	scan.HistoryCommits = messageCount + patchCount
	if limited || patchCount > 256 {
		scan.Limits = appendUnique(scan.Limits, "LOCAL_HISTORY_LIMIT")
	}
	worktrees, limited, err := collisionGit(path, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, scan, err
	}
	if limited {
		scan.Limits = append(scan.Limits, "WORKTREE_LIST_LIMIT")
	}
	for _, field := range bytes.Split(worktrees, []byte{0}) {
		if !bytes.HasPrefix(field, []byte("worktree ")) {
			continue
		}
		if scan.Worktrees >= 32 {
			scan.Limits = append(scan.Limits, "WORKTREE_COUNT_LIMIT")
			break
		}
		scan.Worktrees++
		worktreePath := string(bytes.TrimPrefix(field, []byte("worktree ")))
		abs, _, err := resolveSafeRoot(worktreePath)
		if err != nil {
			scan.Limits = appendUnique(scan.Limits, "WORKTREE_UNREADABLE")
			continue
		}
		root, err := os.OpenRoot(abs)
		if err != nil {
			scan.Limits = appendUnique(scan.Limits, "WORKTREE_UNREADABLE")
			continue
		}
		for _, name := range []string{"TASK.md", ".exord/TASK.md"} {
			if strings.Contains(name, "/") {
				info, err := root.Lstat(".exord")
				if os.IsNotExist(err) {
					continue
				}
				if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					scan.Limits = appendUnique(scan.Limits, "WORKTREE_TASK_UNREADABLE")
					continue
				}
			}
			info, err := root.Lstat(filepath.FromSlash(name))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || !info.Mode().IsRegular() || info.Size() > taskcontract.MaxDocumentBytes {
				scan.Limits = appendUnique(scan.Limits, "WORKTREE_TASK_UNREADABLE")
				continue
			}
			data, err := readRootFile(root, filepath.FromSlash(name), taskcontract.MaxDocumentBytes)
			if err != nil {
				scan.Limits = appendUnique(scan.Limits, "WORKTREE_TASK_UNREADABLE")
				continue
			}
			taskcontract.CollectIDs(data, used)
		}
		root.Close()
	}
	return used, scan, nil
}

func appendUnique(values []string, value string) []string {
	for _, item := range values {
		if item == value {
			return values
		}
	}
	return append(values, value)
}
