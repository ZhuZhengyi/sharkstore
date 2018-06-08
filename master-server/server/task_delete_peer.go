package server

import (
	"fmt"
	"time"
	"util/log"

	"model/pkg/metapb"
	"model/pkg/taskpb"
)

const (
	defaultDelPeerTaskTimeout = time.Second * time.Duration(30)
)

// DeletePeerTask  delete peer task
type DeletePeerTask struct {
	*BaseTask
	peer *metapb.Peer // peer to delete

	confRetries   int
	deleteRetries int
}

// NewDeletePeerTask new delete peer task
func NewDeletePeerTask(id uint64, rangeID uint64, peer *metapb.Peer) *DeletePeerTask {
	return &DeletePeerTask{
		BaseTask: newBaseTask(id, rangeID, TaskTypeDeletePeer, defaultDelPeerTaskTimeout),
		peer:     peer,
	}
}

func (t *DeletePeerTask) String() string {
	return fmt.Sprintf("{%s, \"to_delete\":\"%s\"}", t.BaseTask.String(), t.peer.String())
}

// Step step
func (t *DeletePeerTask) Step(cluster *Cluster, r *Range) (over bool, task *taskpb.Task, err error) {
	if !t.markAsStepping() {
		return
	}
	defer t.unmarkStepping()

	// task is over
	if t.CheckOver() {
		return true, nil, nil
	}

	if r == nil {
		log.Warn("% invalid input range: <nil>", t.loggingID)
		return false, nil, nil
	}

	switch t.GetState() {
	case TaskStateStart:
		over = false
		task = t.stepStart(r)
		return
	case WaitRaftConfReady:
		return t.stepWaitConf(cluster, r)
	case WaitRangeDeleted:
		over, err = t.stepDeleteRange(cluster)
		return
	default:
		err = fmt.Errorf("unexpceted delete peer task state: %s", t.state.String())
	}
	return
}

func (t *DeletePeerTask) issueTask() *taskpb.Task {
	return &taskpb.Task{
		Type: taskpb.TaskType_RangeDelPeer,
		RangeDelPeer: &taskpb.TaskRangeDelPeer{
			Peer: t.peer,
		},
	}
}

func (t *DeletePeerTask) stepStart(r *Range) *taskpb.Task {
	t.state = WaitRaftConfReady
	return t.issueTask()
}

func (t *DeletePeerTask) stepWaitConf(cluster *Cluster, r *Range) (over bool, task *taskpb.Task, err error) {
	if r.GetPeer(t.peer.GetId()) != nil {
		t.confRetries++
		return false, t.issueTask(), nil
	}

	log.Info("%s delete raft member finished, peer: %v", t.loggingID, t.peer)

	over, err = t.stepDeleteRange(cluster)
	return
}

func (t *DeletePeerTask) stepDeleteRange(cluster *Cluster) (over bool, err error) {
	node := cluster.FindNodeById(t.peer.GetNodeId())
	if node == nil {
		log.Warn("%s target node(%d) doesn't exist", t.loggingID, t.peer.GetNodeId())
		t.state = TaskStateCanceled
		return true, nil
	}

	err = cluster.cli.DeleteRange(node.GetServerAddr(), t.rangeID)
	if err == nil {
		log.Error("%s delete range failed, target node: %d, retries: %d", t.loggingID, t.peer.GetNodeId(), t.deleteRetries)
		t.deleteRetries++
		return false, err
	}

	log.Info("%s delete range finished, peer: %v", t.loggingID, t.peer)

	t.state = TaskStateFinished
	return true, nil
}
