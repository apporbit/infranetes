package aws

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/aws/aws-sdk-go/service/ec2"
)

// ReadyError is an information error that tells you why an instance wasn't
// ready.
type ReadyError struct {
	Err error

	ImageID               string
	InstanceID            string
	InstanceType          string
	LaunchTime            time.Time
	PublicIPAddress       string
	State                 string
	StateReason           string
	StateTransitionReason string
	SubnetID              string
	VPCID                 string
}

// Error returns a summarized string version of ReadyError. More details about
// the failed instance can be accessed through the struct.
func (e ReadyError) Error() string {
	return fmt.Sprintf(
		"failed waiting for instance (%s) to be ready, reason was: %s",
		e.InstanceID,
		e.StateReason,
	)
}

func newReadyError(out *ec2.DescribeInstancesOutput) ReadyError {
	if len(out.Reservations) < 1 {
		return ReadyError{Err: ErrNoInstance}
	}
	if len(out.Reservations[0].Instances) < 1 {
		return ReadyError{Err: ErrNoInstance}
	}

	var rerr ReadyError

	if v := out.Reservations[0].Instances[0].ImageId; v != nil {
		rerr.ImageID = *v
	}
	if v := out.Reservations[0].Instances[0].InstanceId; v != nil {
		rerr.InstanceID = *v
	}
	if v := out.Reservations[0].Instances[0].InstanceType; v != nil {
		rerr.InstanceType = *v
	}
	if v := out.Reservations[0].Instances[0].LaunchTime; v != nil {
		rerr.LaunchTime = *v
	}
	if v := out.Reservations[0].Instances[0].PublicIpAddress; v != nil {
		rerr.PublicIPAddress = *v
	}
	if v := out.Reservations[0].Instances[0].State; v != nil {
		if v.Name != nil {
			rerr.State = *v.Name
		}
	}
	if v := out.Reservations[0].Instances[0].StateReason; v != nil {
		if v.Message != nil {
			rerr.StateReason = *v.Message
		}
	}
	if v := out.Reservations[0].Instances[0].StateTransitionReason; v != nil {
		rerr.StateTransitionReason = *v
	}
	if v := out.Reservations[0].Instances[0].SubnetId; v != nil {
		rerr.SubnetID = *v
	}
	if v := out.Reservations[0].Instances[0].VpcId; v != nil {
		rerr.VPCID = *v
	}

	return rerr
}

func waitUntilReady(svc *ec2.EC2, instanceID string) error {
	// With 10 retries, total timeout is about 17 minutes.
	const maxRetries = 10

	var resp *ec2.DescribeInstancesOutput
	var err error

	for i := 0; i < maxRetries; i++ {
		// Sleep will be 1, 2, 4, 8, 16...
		// time.Sleep(2ⁱ * time.Second)
		time.Sleep(time.Duration(math.Exp2(float64(i))) * time.Second)

		resp, err = svc.DescribeInstances(&ec2.DescribeInstancesInput{
			InstanceIds: []*string{&instanceID},
		})
		if err != nil {
			continue
		}

		if len(resp.Reservations) < 1 {
			continue
		}
		if len(resp.Reservations[0].Instances) < 1 {
			continue
		}
		if resp.Reservations[0].Instances[0].State == nil {
			continue
		}
		if resp.Reservations[0].Instances[0].State.Name == nil {
			continue
		}

		state := *resp.Reservations[0].Instances[0].State.Name
		switch state {
		case ec2.InstanceStateNameRunning:
			// We're ready!
			return nil
		case ec2.InstanceStateNameTerminated, ec2.InstanceStateNameStopped,
			ec2.InstanceStateNameStopping, ec2.InstanceStateNameShuttingDown:
			// Polling is useless. This instance isn't coming up. Break the
			// loop.
			i = maxRetries
		}
	}

	rerr := newReadyError(resp)

	if err != nil {
		rerr.Err = err
	} else {
		rerr.Err = errors.New("wait until instance ready timeout")
	}

	return rerr
}
