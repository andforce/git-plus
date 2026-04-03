package eventservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strings"

	"connectrpc.com/connect"
	connectvalidate "connectrpc.com/validate"
	"github.com/ImSingee/git-plus/pkg/eventbus"
	eventv1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/event/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/event/v1/eventv1connect"
	"github.com/ImSingee/git-plus/pkg/task"
	"google.golang.org/protobuf/types/known/structpb"
)

type serviceServer struct {
	bus *eventbus.Bus
}

func NewHandler(bus *eventbus.Bus) http.Handler {
	rpcMux := http.NewServeMux()
	RegisterHandlers(rpcMux, bus)
	return http.StripPrefix("/api", rpcMux)
}

func RegisterHandlers(mux *http.ServeMux, bus *eventbus.Bus) {
	path, handler := eventv1connect.NewEventServiceHandler(
		&serviceServer{bus: bus},
		connect.WithInterceptors(mustValidateInterceptor()),
	)
	mux.Handle(path, handler)
}

func (s *serviceServer) Subscribe(
	ctx context.Context,
	req *connect.Request[eventv1.SubscribeRequest],
	stream *connect.ServerStream[eventv1.SubscribeResponse],
) error {
	channel := strings.TrimSpace(req.Msg.GetChannel())
	if channel != task.TaskChannel {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("channel %q is not supported", channel))
	}

	subscription, events := s.bus.Subscribe(channel)
	defer s.bus.Unsubscribe(subscription)

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				return connect.NewError(connect.CodeUnavailable, errors.New("subscription closed"))
			}

			payload, err := toProtoEventPayload(event.Payload)
			if err != nil {
				return connect.NewError(connect.CodeInternal, fmt.Errorf("marshal event payload: %w", err))
			}
			if err := stream.Send(&eventv1.SubscribeResponse{Event: payload}); err != nil {
				return err
			}
		}
	}
}

func toProtoEventPayload(payload map[string]any) (*structpb.Struct, error) {
	protoPayload, err := structpb.NewStruct(payload)
	if err == nil {
		return protoPayload, nil
	}

	log.Printf("event payload conversion failed, omitting unsupported values: %v", err)
	sanitizedPayload, ok := sanitizeToProtoStruct(payload)
	if !ok {
		return nil, err
	}

	return sanitizedPayload, nil
}

func sanitizeToProtoStruct(payload map[string]any) (*structpb.Struct, bool) {
	fields := make(map[string]*structpb.Value)
	for key, value := range payload {
		protoValue, ok := sanitizeToProtoValue(value)
		if !ok {
			continue
		}
		fields[key] = protoValue
	}

	return &structpb.Struct{Fields: fields}, true
}

func sanitizeToProtoValue(value any) (*structpb.Value, bool) {
	protoValue, err := structpb.NewValue(value)
	if err == nil {
		return protoValue, true
	}

	if value == nil {
		return structpb.NewNullValue(), true
	}

	reflectedValue := reflect.ValueOf(value)
	for reflectedValue.Kind() == reflect.Pointer || reflectedValue.Kind() == reflect.Interface {
		if reflectedValue.IsNil() {
			return structpb.NewNullValue(), true
		}
		reflectedValue = reflectedValue.Elem()
	}

	switch reflectedValue.Kind() {
	case reflect.Map:
		if reflectedValue.Type().Key().Kind() != reflect.String {
			return nil, false
		}

		fields := make(map[string]*structpb.Value)
		iterator := reflectedValue.MapRange()
		for iterator.Next() {
			protoFieldValue, ok := sanitizeToProtoValue(iterator.Value().Interface())
			if !ok {
				continue
			}
			fields[iterator.Key().String()] = protoFieldValue
		}

		return structpb.NewStructValue(&structpb.Struct{Fields: fields}), true
	case reflect.Slice, reflect.Array:
		values := make([]*structpb.Value, 0, reflectedValue.Len())
		for index := range reflectedValue.Len() {
			protoItem, ok := sanitizeToProtoValue(reflectedValue.Index(index).Interface())
			if !ok {
				continue
			}
			values = append(values, protoItem)
		}

		return structpb.NewListValue(&structpb.ListValue{Values: values}), true
	default:
		return nil, false
	}
}

func mustValidateInterceptor() connect.Interceptor {
	interceptor, err := connectvalidate.NewInterceptor()
	if err != nil {
		panic(fmt.Sprintf("create connect validate interceptor: %v", err))
	}

	return interceptor
}
