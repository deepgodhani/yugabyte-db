// Copyright (c) YugaByte, Inc.

package task

import (
	"context"
	"fmt"
	"node-agent/app/task/module"
	pb "node-agent/generated/service"
	"node-agent/util"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

type ServerControlHandler struct {
	taskStatus *atomic.Value
	param      *pb.ServerControlInput
	username   string
}

// ServerControlHandler returns a new instance of ServerControlHandler.
func NewServerControlHandler(param *pb.ServerControlInput, username string) *ServerControlHandler {
	return &ServerControlHandler{param: param, username: username}
}

// CurrentTaskStatus implements the AsyncTask method.
func (handler *ServerControlHandler) CurrentTaskStatus() *TaskStatus {
	// No streaming output during the task.
	return nil
}

// String implements the AsyncTask method.
func (handler *ServerControlHandler) String() string {
	return "runServerControl"
}

// Handle implements the AsyncTask method.
func (handler *ServerControlHandler) Handle(
	ctx context.Context,
) (*pb.DescribeTaskResponse, error) {
	var shellTask *ShellTask
	if handler.param.GetNumVolumes() > 0 {
		cmd := "df | awk '{{print $6}}' | egrep '^/mnt/d[0-9]+' | wc -l"
		util.FileLogger().Infof(ctx, "Running command %v", cmd)
		shellTask = NewShellTaskWithUser(
			handler.String(),
			handler.username,
			util.DefaultShell,
			[]string{"-c", cmd},
		)
		status, err := shellTask.Process(ctx)
		if err != nil {
			util.FileLogger().Errorf(ctx, "Server control failed in %v - %s", cmd, err.Error())
			return nil, err
		}
		count, err := strconv.Atoi(strings.TrimSpace(status.Info.String()))
		if err != nil {
			util.FileLogger().
				Errorf(ctx, "Failed to parse output of command %v - %s", cmd, err.Error())
			return nil, err
		}
		if uint32(count) < handler.param.GetNumVolumes() {
			err = fmt.Errorf(
				"Not all data volumes attached: needed %d found %d",
				handler.param.GetNumVolumes(),
				count,
			)
			util.FileLogger().Errorf(ctx, "Volume mount validation failed - %s", err.Error())
			return nil, err
		}
	}
	controlType := strings.ToLower(pb.ServerControlType_name[int32(handler.param.ControlType)])
	cmd, err := module.ControlServerCmd(
		handler.username,
		handler.param.GetServerName(),
		controlType,
	)
	if err != nil {
		util.FileLogger().Errorf(ctx, "Failed to get server control command - %s", err.Error())
		return nil, err
	}
	shellTask = NewShellTaskWithUser(
		handler.String(),
		handler.username,
		util.DefaultShell,
		[]string{"-c", cmd},
	)
	util.FileLogger().Infof(ctx, "Running command %v", cmd)
	_, err = shellTask.Process(ctx)
	if err != nil {
		util.FileLogger().Errorf(ctx, "Server control failed in %v - %s", cmd, err.Error())
		return nil, err
	}
	if handler.param.GetDeconfigure() {
		confFilepath := filepath.Join(handler.param.GetServerHome(), "conf", "server.conf")
		util.FileLogger().Infof(ctx, "Removing server conf file %s", confFilepath)
		err = os.Remove(confFilepath)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return &pb.DescribeTaskResponse{
		Data: &pb.DescribeTaskResponse_ServerControlOutput{
			// TODO set pid.
			ServerControlOutput: &pb.ServerControlOutput{},
		},
	}, nil
}
