package scheduler

import (
	"net/http"

	"github.com/Dataman-Cloud/swan/mesosproto/mesos"
	"github.com/Dataman-Cloud/swan/mesosproto/sched"
	"github.com/Dataman-Cloud/swan/types"

	"github.com/Sirupsen/logrus"
	"github.com/golang/protobuf/proto"
)

func (s *Scheduler) BuildTask(offer *mesos.Offer, resources []*mesos.Resource, task *types.Task) *mesos.TaskInfo {
	logrus.Infof("Prepared task for launch with offer %s", *offer.GetId().Value)
	taskInfo := mesos.TaskInfo{
		Name: proto.String(task.Name),
		TaskId: &mesos.TaskID{
			Value: proto.String(task.ID),
		},
		AgentId:   offer.AgentId,
		Resources: resources,
		Command: &mesos.CommandInfo{
			Shell: proto.Bool(false),
			Value: nil,
		},
		Container: &mesos.ContainerInfo{
			Type: mesos.ContainerInfo_DOCKER.Enum(),
			Docker: &mesos.ContainerInfo_DockerInfo{
				Image: task.Image,
			},
		},
	}

	if task.Privileged != nil {
		taskInfo.Container.Docker.Privileged = task.Privileged
	}

	if task.ForcePullImage != nil {
		taskInfo.Container.Docker.ForcePullImage = task.ForcePullImage
	}

	for _, parameter := range task.Parameters {
		taskInfo.Container.Docker.Parameters = append(taskInfo.Container.Docker.Parameters, &mesos.Parameter{
			Key:   proto.String(parameter.Key),
			Value: proto.String(parameter.Value),
		})
	}

	for _, volume := range task.Volumes {
		mode := mesos.Volume_RO
		if volume.Mode == "RW" {
			mode = mesos.Volume_RW
		}
		taskInfo.Container.Volumes = append(taskInfo.Container.Volumes, &mesos.Volume{
			ContainerPath: proto.String(volume.ContainerPath),
			HostPath:      proto.String(volume.HostPath),
			Mode:          &mode,
		})
	}

	vars := make([]*mesos.Environment_Variable, 0)
	for k, v := range task.Env {
		vars = append(vars, &mesos.Environment_Variable{
			Name:  proto.String(k),
			Value: proto.String(v),
		})
	}

	taskInfo.Command.Environment = &mesos.Environment{
		Variables: vars,
	}

	if task.Labels != nil {
		labels := make([]*mesos.Label, 0)
		for k, v := range *task.Labels {
			labels = append(labels, &mesos.Label{
				Key:   proto.String(k),
				Value: proto.String(v),
			})
		}

		taskInfo.Labels = &mesos.Labels{
			Labels: labels,
		}
	}

	switch task.Network {
	case "NONE":
		taskInfo.Container.Docker.Network = mesos.ContainerInfo_DockerInfo_NONE.Enum()
	case "HOST":
		taskInfo.Container.Docker.Network = mesos.ContainerInfo_DockerInfo_HOST.Enum()
	case "BRIDGE":
		ports := s.GetPorts(offer)
		for _, m := range task.PortMappings {
			hostPort := ports[s.taskLaunched]
			//ports = ports[1:]
			taskInfo.Container.Docker.PortMappings = append(taskInfo.Container.Docker.PortMappings,
				&mesos.ContainerInfo_DockerInfo_PortMapping{
					HostPort:      proto.Uint32(uint32(hostPort)),
					ContainerPort: proto.Uint32(m.Port),
					Protocol:      proto.String(m.Protocol),
				},
			)
			taskInfo.Resources = append(taskInfo.Resources, &mesos.Resource{
				Name: proto.String("ports"),
				Type: mesos.Value_RANGES.Enum(),
				Ranges: &mesos.Value_Ranges{
					Range: []*mesos.Value_Range{
						{
							Begin: proto.Uint64(uint64(hostPort)),
							End:   proto.Uint64(uint64(hostPort)),
						},
					},
				},
			})
		}
		taskInfo.Container.Docker.Network = mesos.ContainerInfo_DockerInfo_BRIDGE.Enum()
	default:
		taskInfo.Container.Docker.Network = mesos.ContainerInfo_DockerInfo_NONE.Enum()
	}

	return &taskInfo
}

// LaunchTasks lauch multiple tasks with specified offer.
func (s *Scheduler) LaunchTasks(offer *mesos.Offer, tasks []*mesos.TaskInfo) (*http.Response, error) {
	logrus.Infof("Launch %d tasks with offer %s", len(tasks), *offer.GetId().Value)
	call := &sched.Call{
		FrameworkId: s.framework.GetId(),
		Type:        sched.Call_ACCEPT.Enum(),
		Accept: &sched.Call_Accept{
			OfferIds: []*mesos.OfferID{
				offer.GetId(),
			},
			Operations: []*mesos.Offer_Operation{
				&mesos.Offer_Operation{
					Type: mesos.Offer_Operation_LAUNCH.Enum(),
					Launch: &mesos.Offer_Operation_Launch{
						TaskInfos: tasks,
					},
				},
			},
			Filters: &mesos.Filters{RefuseSeconds: proto.Float64(1)},
		},
	}

	return s.send(call)
}

func (s *Scheduler) KillTask(agentId, taskId string) (*http.Response, error) {
	logrus.WithFields(logrus.Fields{"ID": taskId, "AgentId": agentId}).Warn("Kill Task")
	call := &sched.Call{
		FrameworkId: s.framework.GetId(),
		Type:        sched.Call_KILL.Enum(),
		Kill: &sched.Call_Kill{
			TaskId: &mesos.TaskID{
				Value: proto.String(taskId),
			},
			AgentId: &mesos.AgentID{
				Value: proto.String(agentId),
			},
		},
	}

	return s.send(call)
}
