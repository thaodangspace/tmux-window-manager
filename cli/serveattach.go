package cli

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/thaodangspace/tmux-window-manager/attachlink"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
	"github.com/spf13/cobra"
)

const maxAttachTargetBytes = 64

func newServeAttachCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "serve-attach",
		Short:  "Run one private tmux attach helper",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServeAttach(cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	return cmd
}

func runServeAttach(in io.Reader, out io.Writer) error {
	target, err := readAttachTarget(in)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server, err := attachlink.Listen(ctx, target, attachlink.ProductionTmux(), attachlink.DefaultLifetime)
	if err != nil {
		return err
	}
	defer server.Close()
	if _, err := io.WriteString(out, "READY "+server.URL()+"\n"); err != nil {
		return err
	}
	server.Wait()
	return nil
}

func readAttachTarget(in io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(in, maxAttachTargetBytes)).ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", errors.New("attach target is required")
	}
	target := strings.TrimSpace(line)
	if !attachTargetValid(target) {
		return "", attachlink.ErrInvalidTarget
	}
	return target, nil
}

func attachTargetValid(target string) bool {
	return tmuxcli.ValidPaneID(target)
}
