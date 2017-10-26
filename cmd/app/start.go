package app

import (
	"io"

	"github.com/spf13/cobra"
)

func NewCommandStartSampleAdapterServer(out, errOut io.Writer, stopCh <-chan struct{}) *cobra.Command {
	return nil
}
