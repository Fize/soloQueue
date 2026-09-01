package channel

import "github.com/xiaobaitu/soloqueue/internal/runwatch"

// UserFacingError returns a stable message suitable for a channel reply.
// Internal causes and operation identifiers belong in logs and diagnostics,
// never in user-facing message bodies.
func UserFacingError(err error) string {
	switch runwatch.CodeOf(err) {
	case runwatch.CodeModelTransportStalled,
		runwatch.CodeModelFirstProgressStalled,
		runwatch.CodeModelSemanticStalled:
		return "The model stopped responding. Please try again."
	case runwatch.CodeToolStalled:
		return "A tool stopped responding. Please try again."
	case runwatch.CodeDelegationOrphaned:
		return "The delegated task stopped responding. Please try again."
	case runwatch.CodeRootOrphaned:
		return "The task stopped responding. Please try again."
	default:
		return "The request could not be completed. Please try again."
	}
}
