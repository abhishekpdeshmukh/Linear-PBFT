package main

import (
	"fmt"
	"time"
)

type ViewChangeTracker struct {
	viewChangeCount       int
	viewChangeStartChan   chan struct{} // Channel to signal 2f prepares
	doneChan              chan struct{} // Channel to indicate timeout
	viewChangeSuccessChan chan struct{} // Channel to signal 3f+1 prepares
}
type PrepareTracker struct {
	prepareCount int
	prepareChan  chan struct{} // Channel to signal 2f prepares
	doneChan     chan struct{} // Channel to indicate timeout
	quorumChan   chan struct{} // Channel to signal 3f+1 prepares
	timeout      time.Duration // Timeout duration
	// f            int           // Fault tolerance level
}

type CommitTracker struct {
	commitCount int
	commitChan  chan struct{} // Channel to signal 2f prepares
	doneChan    chan struct{} // Channel to indicate timeout
	quorumChan  chan struct{} // Channel to signal 3f+1 prepares
	timeout     time.Duration // Timeout duration
	// f           int           // Fault tolerance level
}
type CheckPointTracker struct {
	checkPointCount int
	checkPointChan  chan struct{} // Channel to signal 2f prepares
	doneChan        chan struct{} // Channel to indicate timeout
	quorumChan      chan struct{} // Channel to signal 3f+1 prepares
	timeout         time.Duration // Timeout duration
	// f            int           // Fault tolerance level
}

func NewViewChangeTracker() *ViewChangeTracker {
	return &ViewChangeTracker{
		viewChangeCount:       0,
		viewChangeStartChan:   make(chan struct{}, 1), // Channel to signal 2f prepares
		doneChan:              make(chan struct{}, 1), // Channel to indicate timeout
		viewChangeSuccessChan: make(chan struct{}, 1),
	}
}
func NewPrepareTracker(timeout time.Duration) *PrepareTracker {
	return &PrepareTracker{
		prepareCount: 0,
		prepareChan:  make(chan struct{}, 1),
		doneChan:     make(chan struct{}, 1),
		quorumChan:   make(chan struct{}, 1),
		timeout:      timeout,
		// f:            f,
	}
}

func NewCommitTracker(timeout time.Duration) *CommitTracker {
	return &CommitTracker{
		commitCount: 0,
		commitChan:  make(chan struct{}, 1),
		doneChan:    make(chan struct{}, 1),
		quorumChan:  make(chan struct{}, 1),
		timeout:     timeout,
		// f:           f,
	}
}

func NewCheckPointTracker(timeout time.Duration) *CheckPointTracker {
	return &CheckPointTracker{
		checkPointCount: 0,
		checkPointChan:  make(chan struct{}, 1),
		doneChan:        make(chan struct{}, 1),
		quorumChan:      make(chan struct{}, 1),
		timeout:         timeout,
		// f:           f,
	}
}

func (node *Node) checkViewChangeThresholds(tracker *ViewChangeTracker, nextView int) {
	fmt.Println("Inside View Change Tracker my current count is ", tracker.viewChangeCount)
	if node.timer != nil {
		return
	}
	if tracker.viewChangeCount >= 2*F+1 && node.isViewChangeProcess {
		fmt.Println("Restarting the timer waiting for NEWVIEW MSG")
		node.view = int32(nextView)
		fmt.Println("My current View is ", node.view)
		fmt.Println("Next view is ", nextView, " and My node id is ", node.nodeID)
		fmt.Println("Next Leader should be ", node.ServerMapping[int32(nextView)%N])
		if node.ServerMapping[int32(nextView)%N] == node.nodeID {
			fmt.Println("As a Leader")
			node.isLeader = true
			node.viewChangeTracker.viewChangeCount = 0
			go node.sendNewView(nextView)
			go node.startOrResetTimer(time.Second * 5)
			node.isViewChangeProcess = false
		} else {
			fmt.Println("I should not be the leader but i should start the timer ")
			fmt.Println("My current Leeader status before turning to false is ", node.isLeader)
			node.isLeader = false
			go node.startOrResetTimer(time.Second * 5)
			node.viewChangeTracker.viewChangeCount = 0
			node.isViewChangeProcess = false
		}

	} else if tracker.viewChangeCount >= F+1 {
		fmt.Println("Inside this condition of F+1")
		fmt.Println("My current view ", node.view)
		if !node.isViewChangeProcess {
			node.isViewChangeProcess = true
			fmt.Println("Hello I am staring a view change cause i got F+1 view change msgs")
			go node.initiateViewChange()
		}

		fmt.Println("I am in a ongoing ViewChange process sorry cant initiate anything")
		// select {
		// case tracker.commitChan <- struct{}{}:
		// 	fmt.Println("Received 2f+1 Commit messages, sending response to client")
		// 	// sendResponseToClient(node, sequenceNum)
		// default:
		// }
	}
}

// Helper function to check Commit thresholds
func (node *Node) checkCommitThresholds(sequenceNum int, tracker *CommitTracker) {
	if tracker.commitCount >= 2*F {
		select {
		case tracker.commitChan <- struct{}{}:
			fmt.Println("Received 2f+1 Commit messages, sending response to client")
			// sendResponseToClient(node, sequenceNum)
		default:
		}
	}
}

// Function to monitor Commit timeout
func (node *Node) monitorCommitTimeout(sequenceNum int) {
	tracker := node.commitTrackers[sequenceNum]
	select {
	case <-time.After(tracker.timeout):
		fmt.Println("Timeout: Commit messages not received in time for sequence number", sequenceNum)
		// Handle timeout
	case <-tracker.commitChan:
		fmt.Println("Commit successful for sequence number", sequenceNum)
		// Commit was successful
		logEntry := node.logs[sequenceNum]
		logEntry.isCommitted = true
		logEntry.status = "C"
		node.logs[sequenceNum] = logEntry
		node.notifyCh <- struct{}{}
	}
}
func (node *Node) monitorCheckpointTimeout(sequenceNum int) {
	tracker := node.checkpointTracker[sequenceNum]
	select {
	case <-time.After(tracker.timeout):
		fmt.Println("Check Point Msgs not recieved on time", sequenceNum)
		// Handle timeout
	case <-tracker.checkPointChan:
		fmt.Println("Checkpoint successful for sequence number", sequenceNum)
		node.lastStableCheckpoint = sequenceNum
		fmt.Println("Now the last stable checkpoint is ", node.lastStableCheckpoint)
	}
}

// Check prepare thresholds for 2f and 3f+1
func (node *Node) checkPrepareThresholds(tracker *PrepareTracker) {
	if tracker.prepareCount >= 3*F {
		select {
		case tracker.quorumChan <- struct{}{}:
			fmt.Println("Received 3f Prepare messages")
		default:
		}
	}
}
func (node *Node) checkPointThresholds(tracker *CheckPointTracker) {
	if tracker.checkPointCount >= 2*F {
		select {
		case tracker.quorumChan <- struct{}{}:
			fmt.Println("Received 2f+1 Checkpoint messages")

		default:
		}
	}
}

// Monitor prepare timeout and take actions based on received Prepare counts
func (node *Node) monitorPrepareTimeout(sequenceNum int) {
	tracker := node.prepareTrackers[sequenceNum]
	done := make(chan struct{}) // Channel to cancel timeout on action

	select {
	case <-time.After(tracker.timeout):
		// Timeout case will only execute if `done` channel isn't closed
		select {
		case <-done:
			// Timeout was canceled, do nothing
			return
		default:
			// Timeout reached
			node.lock.Lock()
			defer node.lock.Unlock()

			if tracker.prepareCount >= 3*F {
				fmt.Println("Timeout: 3f Prepare messages received, proceeding with commit")
				// Action for 3f case
				sendCollectedPrepare(node, sequenceNum)
			} else if tracker.prepareCount >= 2*F {
				fmt.Println("Timeout: Only 2f Prepare messages received, taking partial action")
				// Action for 2f case
				sendCollectedPrepare(node, sequenceNum)
			} else {
				fmt.Println("Timeout: Insufficient Prepare messages, aborting")
				// Action for insufficient prepares
			}

			close(tracker.doneChan) // Signal that the timeout has completed
		}

	case <-tracker.quorumChan:
		// 3f Prepare messages received before timeout, committing immediately
		close(done) // Cancel timeout
		node.lock.Lock()
		defer node.lock.Unlock()
		fmt.Println("3f Prepare messages received before timeout, committing immediately")
		sendCollectedPrepare(node, sequenceNum)

	case <-tracker.prepareChan:
		// 2f Prepare messages received, but not enough for immediate commit
		// close(done) // Cancel timeout
		// node.lock.Lock()
		// defer node.lock.Unlock()
		// fmt.Println("2f Prepare messages received, processing accordingly")
		// // Action for 2f prepares
	}
}
