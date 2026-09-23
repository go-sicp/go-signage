package signagegrpc

// PullController is the contract the agent main exposes to the gRPC service
// so SetPullMode/GetPullMode RPCs can reconfigure the pull goroutine at
// runtime. Implementations live in signagepull.
//
// It is optional: passing a nil PullController to NewDisplayService makes
// the two RPCs return a FailedPrecondition status, which is the right
// behaviour on a push-only deployment.
type PullController interface {
	SetPullMode(enabled bool, contentAddr, deviceID string, intervalSec int) error
	GetPullMode() PullModeStatus
}

// PullModeStatus is the domain mirror of pb.PullModeStatus. It avoids
// leaking the generated proto type into signagepull.
type PullModeStatus struct {
	Enabled             bool
	ContentServerAddr   string
	DeviceID            string
	PollIntervalSeconds int
	LastPullUnix        int64
	LastFrameHash       string
	LastError           string
}
