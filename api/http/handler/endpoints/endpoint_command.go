package endpoints

import (
	"bytes"
	"context"
	"net/http"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/strslice"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
	"github.com/portainer/portainer"
	"github.com/portainer/portainer/archive"
)

type endpointCommandPayload struct {
	Script []byte
}

func (payload *endpointCommandPayload) Validate(r *http.Request) error {
	script, _, err := request.RetrieveMultiPartFormFile(r, "Script")
	if err != nil {
		return portainer.Error("Invalid Script file. Ensure that the file is uploaded correctly")
	}
	payload.Script = script

	return nil
}

// POST request on /api/endpoints/:id/command
func (handler *Handler) endpointCommand(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	endpointID, err := request.RetrieveNumericRouteVariableValue(r, "id")
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid endpoint identifier route variable", err}
	}

	payload := &endpointCommandPayload{}
	err = payload.Validate(r)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
	}

	endpoint, err := handler.EndpointService.Endpoint(portainer.EndpointID(endpointID))
	if err == portainer.ErrObjectNotFound {
		return &httperror.HandlerError{http.StatusNotFound, "Unable to find an endpoint with the specified identifier inside the database", err}
	} else if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to find an endpoint with the specified identifier inside the database", err}
	}

	// TODO: commented for test purposes
	// err = handler.requestBouncer.EndpointAccess(r, endpoint)
	// if err != nil {
	// 	return &httperror.HandlerError{http.StatusForbidden, "Permission denied to access endpoint", portainer.ErrEndpointAccessDenied}
	// }

	buffer, err := archive.TarFileInBuffer(payload.Script, "script.sh", 0700)
	if err != nil {
		// TODO: better error handling in all handler
		return &httperror.HandlerError{http.StatusInternalServerError, "Command exec error", err}
	}

	cli, err := handler.DockerClientFactory.CreateClient(endpoint)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Command exec error", err}
	}
	defer cli.Close()

	// containerName := "test"

	cmd := make([]string, 1)
	cmd[0] = "/tmp/script.sh"

	containerConfig := &container.Config{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		WorkingDir:   "/tmp",
		// TODO: image must be available on the endpoint, missing pull step right now
		Image: "ubuntu:14.04",
		// TODO: can be useful to specify an id there? task exec id?
		Labels: map[string]string{"io.portainer.command": "someidentifier"},
		Cmd:    strslice.StrSlice(cmd),
	}

	hostConfig := &container.HostConfig{
		// TODO: want to define all sys bind mounts here (/usr, /etc, ...)
		Binds:       []string{"/:/host", "/etc:/etc:ro"},
		NetworkMode: "host",
		Privileged:  true,
		// Mounts: []mount.Mount{
		// 	mount.Mount{
		// 		Type:     mount.TypeBind,
		// 		Source:   "/",
		// 		Target:   "/host",
		// 		ReadOnly: false,
		// 	},
		// 	mount.Mount{
		// 		Type:     mount.TypeBind,
		// 		Source:   "/etc",
		// 		Target:   "/etc",
		// 		ReadOnly: true,
		// 	},
		// },
	}

	networkConfig := &network.NetworkingConfig{}

	body, err := cli.ContainerCreate(context.Background(), containerConfig, hostConfig, networkConfig, "")
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Command exec (create) error", err}
	}

	copyOptions := types.CopyToContainerOptions{}
	err = cli.CopyToContainer(context.Background(), body.ID, "/tmp", bytes.NewReader(buffer), copyOptions)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Command exec (copy) error", err}
	}

	startOptions := types.ContainerStartOptions{}
	err = cli.ContainerStart(context.Background(), body.ID, startOptions)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Command exec (start) error", err}
	}

	return response.Empty(w)
}
