/**
 * Copyright © 2022 Hamed Yousefi <hdyousefi@gmail.com>.
 */

package core

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hmdsefi/channelize/internal/channel"
	"github.com/hmdsefi/channelize/internal/common"
	"github.com/hmdsefi/channelize/internal/common/errorx"
	"github.com/hmdsefi/channelize/internal/common/log"
	"github.com/hmdsefi/channelize/internal/core/mock"
)

var (
	expectedData = data{"John", "Doe"}
)

type data struct {
	Firstname string `json:"firstname"`
	Lastname  string `json:"lastname"`
}

type testMessageOut struct {
	Channel channel.Channel `json:"channel"`
	Data    data            `json:"data"`
}

// TestDispatch_SendPublicMessage send a public message to a channel. The dispatch
// storage has only one connection for that channel.
func TestDispatch_SendPublicMessage(t *testing.T) {
	const (
		testChannel = channel.Channel("testChannel")
		connID      = "test-conn-id"
	)

	ctx := context.Background()

	conn := mock.NewConnection(connID, nil, authNoopFunc)
	dispatch := NewDispatch(mock.NewStore([]common.ConnectionWrapper{conn}), log.NewDefaultLogger())

	err := dispatch.SendPublicMessage(ctx, testChannel, expectedData)
	if err != nil {
		t.Fatal(err)
	}

	msgOutBytes := <-conn.Message()

	var msgOut testMessageOut
	err = json.Unmarshal(msgOutBytes, &msgOut)
	require.Nil(t, err)

	assert.Equal(t, testChannel, msgOut.Channel)
	assert.Equal(t, expectedData, msgOut.Data)
}

// TestDispatch_SendPublicMessage_Concurrent creates a list of connection and
// store them in the dispatch storage for any input channel. Sends multiple
// public messages and reads them concurrently.
func TestDispatch_SendPublicMessage_Concurrent(t *testing.T) {
	const (
		testChannel = channel.Channel("testChannel")
	)

	ctx := context.Background()

	// create list of mock connections.
	var mockConnections []*mock.Connection
	var connections []common.ConnectionWrapper
	for _, id := range testConnectionIDs {
		conn := mock.NewConnection(id, nil, authNoopFunc)
		mockConnections = append(mockConnections, conn)
		connections = append(connections, conn)
	}

	wg := sync.WaitGroup{}
	wg.Add(len(connections))

	// read from all available connection concurrently.
	for i := range connections {
		idx := i
		go func() {
			defer wg.Done()
			for msgOutBytes := range mockConnections[idx].Message() {
				var msgOut testMessageOut
				err := json.Unmarshal(msgOutBytes, &msgOut)
				require.Nil(t, err)

				assert.Equal(t, testChannel, msgOut.Channel)
				assert.Equal(t, expectedData, msgOut.Data)
			}
		}()
	}

	// store the created connections into the storage and create dispatch with it.
	dispatch := NewDispatch(mock.NewStore(connections), log.NewDefaultLogger())

	// send multiple public messages concurrently
	parallelSendCount := 100
	sendWG := sync.WaitGroup{}
	sendWG.Add(parallelSendCount)
	for i := 0; i < parallelSendCount; i++ {
		go func() {
			defer sendWG.Done()
			err := dispatch.SendPublicMessage(ctx, testChannel, expectedData)
			require.Nil(t, err)
		}()
	}

	// wait until dispatch sends all the public messages then close the
	// connections to stop reading messages.
	sendWG.Wait()
	for i := range mockConnections {
		mockConnections[i].Close()
	}

	// wait for the goroutines that are reading the messages to be done.
	wg.Wait()
}

// TestDispatch_SendPrivateMessage send a private message to the client's subscribed
// private channel.
func TestDispatch_SendPrivateMessage(t *testing.T) {
	const (
		privateChannel = channel.Channel("testPrivateChannel")
		connID         = "test-conn-id"
		userID         = "test_user_id"
	)

	ctx := context.Background()

	conn := mock.NewConnection(connID, nil, authNoopFunc)
	dispatch := NewDispatch(mock.NewStore([]common.ConnectionWrapper{conn}), log.NewDefaultLogger())

	err := dispatch.SendPrivateMessage(ctx, privateChannel, userID, expectedData)
	if err != nil {
		t.Fatal(err)
	}

	msgOutBytes := <-conn.Message()

	var msgOut testMessageOut
	err = json.Unmarshal(msgOutBytes, &msgOut)
	require.Nil(t, err)

	assert.Equal(t, privateChannel, msgOut.Channel)
	assert.Equal(t, expectedData, msgOut.Data)
}

// TestDispatch_SendPrivateMessage_AuthError send a private message to the client's
// subscribed private channel. The token is expired.
func TestDispatch_SendPrivateMessage_AuthError(t *testing.T) {
	const (
		privateChannel = channel.Channel("testPrivateChannel")
		connID         = "test-conn-id"
		userID         = "test_user_id"
	)

	ctx := context.Background()

	conn := mock.NewConnection(connID, nil, makeAuthFunc(errorx.CodeAuthTokenIsExpired))
	mockStore := mock.NewStore([]common.ConnectionWrapper{conn})
	dispatch := NewDispatch(mockStore, log.NewDefaultLogger())

	err := dispatch.SendPrivateMessage(ctx, privateChannel, userID, expectedData)
	require.NotNil(t, err)
	assert.Equal(t, errorx.NewChannelizeError(errorx.CodeAuthTokenIsExpired).Error(), err.Error())
	assert.Equal(t, privateChannel.String(), mockStore.Receive())
}

func makeAuthFunc(errorCode int) func() error {
	return func() error {
		return errorx.NewChannelizeError(errorCode)
	}
}

// TestDispatch_SendPrivateMessage_Concurrent sends multiple concurrent messages
// to the multiple clients' subscribed private channel.
func TestDispatch_SendPrivateMessage_Concurrent(t *testing.T) {
	const (
		privateChannel = channel.Channel("testPrivateChannel")
	)

	ctx := context.Background()

	// create list of mock connections.
	var mockConnections []*mock.Connection
	var connections []common.ConnectionWrapper
	userIDs := make([]string, len(testConnectionIDs))
	for i, id := range testConnectionIDs {
		userIDs[i] = uuid.NewV4().String()
		conn := mock.NewConnection(id, &userIDs[i], authNoopFunc)
		mockConnections = append(mockConnections, conn)
		connections = append(connections, conn)
	}

	wg := sync.WaitGroup{}
	wg.Add(len(connections))

	// read from all available connection concurrently.
	for i := range connections {
		idx := i
		go func() {
			defer wg.Done()
			for msgOutBytes := range mockConnections[idx].Message() {
				var msgOut testMessageOut
				err := json.Unmarshal(msgOutBytes, &msgOut)
				require.Nil(t, err)

				assert.Equal(t, privateChannel, msgOut.Channel)
				assert.Equal(t, expectedData, msgOut.Data)
			}
		}()
	}

	// store the created connections into the storage and create dispatch with it.
	dispatch := NewDispatch(mock.NewStore(connections), log.NewDefaultLogger())

	// send multiple private messages concurrently
	parallelSendCount := 100
	sendWG := sync.WaitGroup{}
	sendWG.Add(parallelSendCount * len(userIDs))
	for i := 0; i < parallelSendCount; i++ {
		for j := range userIDs {
			userID := userIDs[j]
			go func() {
				defer sendWG.Done()
				err := dispatch.SendPrivateMessage(ctx, privateChannel, userID, expectedData)
				require.Nil(t, err)
			}()
		}
	}

	// wait until dispatch sends all the private messages then close the
	// connections to stop reading messages.
	sendWG.Wait()
	for i := range mockConnections {
		mockConnections[i].Close()
	}

	// wait for the goroutines that are reading the messages to be done.
	wg.Wait()
}
