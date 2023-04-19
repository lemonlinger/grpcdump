package grpchelper

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/bojand/ghz/protodesc"
	"github.com/fullstorydev/grpcurl"
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/desc/protoparse"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
)

type ProtoParser interface {
	MarshalRequest(path string, message []byte) (*dynamic.Message, error)
	MarshalResponse(path string, message []byte) (*dynamic.Message, error)
	GetPathFilenames() map[string]string
	GetAllPaths() []string
}

type parser struct {
	Paths     map[string]string
	Requests  map[string]*desc.MessageDescriptor
	Responses map[string]*desc.MessageDescriptor
}

func NewProtoParserFromReflection(addr string) (_ ProtoParser, err error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	refClient := grpcreflect.NewClient(ctx, rpb.NewServerReflectionClient(conn))
	reflSource := grpcurl.DescriptorSourceFromServer(ctx, refClient)

	services, err := reflSource.ListServices()
	if err != nil {
		return nil, err
	}

	paths := make(map[string]string)
	requests := make(map[string]*desc.MessageDescriptor)
	response := make(map[string]*desc.MessageDescriptor)
	for _, service := range services {
		methods, err := grpcurl.ListMethods(reflSource, service)
		if err != nil {
			return nil, err
		}

		for _, method := range methods {
			methodDesc, err := protodesc.GetMethodDescFromReflect(method, refClient)
			if err != nil {
				return nil, err
			}

			path := fmt.Sprintf("/%s/%s", service, methodDesc.GetName())
			paths[path] = path
			requests[path] = methodDesc.GetInputType()
			response[path] = methodDesc.GetOutputType()
		}
	}
	return &parser{
		Paths:     paths,
		Requests:  requests,
		Responses: response,
	}, nil
}

func NewProtoParser(filenames []string) (_ ProtoParser, err error) {
	paths := make(map[string]string)
	requests := make(map[string]*desc.MessageDescriptor)
	response := make(map[string]*desc.MessageDescriptor)

	for _, filename := range filenames {
		if filename, err = filepath.Abs(filename); err != nil {
			return
		}
		dir, base := filepath.Dir(filename), filepath.Base(filename)
		fileNames, err := protoparse.ResolveFilenames([]string{dir}, base)
		if err != nil {
			return nil, err
		}
		p := protoparse.Parser{
			ImportPaths:           []string{dir},
			IncludeSourceCodeInfo: true,
		}
		parsedFiles, err := p.ParseFiles(fileNames...)
		if err != nil {
			return nil, err
		}

		if len(parsedFiles) < 1 {
			err = errors.New("proto file not found")
			return nil, err
		}

		for _, parsedFile := range parsedFiles {
			for _, service := range parsedFile.GetServices() {
				serviceName := fmt.Sprintf("%s.%s", parsedFile.GetPackage(), service.GetName())
				for _, method := range service.GetMethods() {
					path := fmt.Sprintf("/%s/%s", serviceName, method.GetName())
					paths[path] = filepath.Join(dir, parsedFile.GetName())
					requests[path] = method.GetInputType()
					response[path] = method.GetOutputType()
				}
			}
		}
	}
	return &parser{
		Paths:     paths,
		Requests:  requests,
		Responses: response,
	}, nil
}

func (p *parser) MarshalRequest(path string, message []byte) (*dynamic.Message, error) {
	descriptor, ok := p.Requests[path]
	if !ok {
		return nil, fmt.Errorf("path not found: %s", path)
	}
	msg := dynamic.NewMessage(descriptor)
	return msg, msg.Unmarshal(message)
}

func (p *parser) MarshalResponse(path string, message []byte) (*dynamic.Message, error) {
	descriptor, ok := p.Responses[path]
	if !ok {
		return nil, fmt.Errorf("path not found: %s", path)
	}
	msg := dynamic.NewMessage(descriptor)
	return msg, msg.Unmarshal(message)
}

func (p *parser) GetPathFilenames() map[string]string {
	return p.Paths
}

func (p *parser) GetAllPaths() (paths []string) {
	for path := range p.Paths {
		paths = append(paths, path)
	}
	return
}
