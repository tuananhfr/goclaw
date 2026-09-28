package pipeline

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// IsolateContext permits explicit input and tool results, without shared memory or prompt hooks.
// Identity scoping and the input guard remain in InjectContext.
func (d *PipelineDeps) IsolateContext() {
	d.Hooks = nil
	d.LoadContextFiles = nil
	d.LoadSessionHistory = nil
	d.AutoInject = nil
	d.InjectReminders = nil
	d.EnrichMedia = nil
	d.DrainInjectCh = nil
	d.RunMemoryFlush = nil
	d.FlushMessages = nil
	d.MaybeSummarize = nil
	d.EmitSessionCompleted = nil
	d.UpdateMetadata = nil
	d.BootstrapCleanup = nil
	d.SkillPostscript = nil
	d.PersistAssistantImages = nil
	d.BuildMessages = func(_ context.Context, input *RunInput, _ []providers.Message, _ string) ([]providers.Message, error) {
		return []providers.Message{
			{Role: "system", Content: input.ExtraSystemPrompt},
			{Role: "user", Content: input.Message},
		}, nil
	}
}
