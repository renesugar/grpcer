// Copyright 2016 Tamás Gulácsi
//
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

// protoc-gen-grpc generates a grpcer.Client from the given protoc file.
package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	protoc "github.com/golang/protobuf/protoc-gen-go/plugin"
)

func main() {
	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	var req protoc.CodeGeneratorRequest
	if err = proto.Unmarshal(data, &req); err != nil {
		log.Fatal(err)
	}

	var resp protoc.CodeGeneratorResponse
	if err := Generate(&resp, req); err != nil {
		log.Fatal(err)
	}
	data, err = proto.Marshal(&resp)
	if err != nil {
		log.Fatal(err)
	}
	if _, err = os.Stdout.Write(data); err != nil {
		log.Fatal(err)
	}
}

func Generate(resp *protoc.CodeGeneratorResponse, req protoc.CodeGeneratorRequest) error {
	destPkg, destDir := "main", "."
	for _, elt := range strings.Split(req.GetParameter(), ",") {
		if i := strings.IndexByte(elt, '='); i >= 0 {
			k, v := elt[:i], elt[i+1:]
			switch k {
			case "package":
				destPkg = v
			case "path":
				destDir = v
			}
		}
	}
	log.Printf("parameter=%q => destPkg=%q destDir=%q", req.GetParameter(), destPkg, destDir)

	// Find roots.
	rootNames := req.GetFileToGenerate()
	files := req.GetProtoFile()
	roots := make(map[string]*descriptor.FileDescriptorProto, len(rootNames))
	allTypes := make(map[string]*descriptor.DescriptorProto, 1024)
	var found int
	for i := len(files) - 1; i >= 0; i-- {
		f := files[i]
		for _, m := range f.GetMessageType() {
			allTypes["."+f.GetPackage()+"."+m.GetName()] = m
		}
		if found == len(rootNames) {
			continue
		}
		for _, root := range rootNames {
			if f.GetName() == root {
				roots[root] = files[i]
				found++
				break
			}
		}
	}

	msgTypes := make(map[string]*descriptor.DescriptorProto, len(allTypes))
	for _, root := range roots {
		//k := "." + root.GetName() + "."
		var k string
		for _, svc := range root.GetService() {
			for _, m := range svc.GetMethod() {
				if kk := k + m.GetInputType(); len(kk) > len(k) {
					msgTypes[kk] = allTypes[kk]
				}
				if kk := k + m.GetOutputType(); len(kk) > len(k) {
					msgTypes[kk] = allTypes[kk]
				}
			}
		}
	}

	var grp errgroup.Group
	resp.File = make([]*protoc.CodeGeneratorResponse_File, 0, len(roots))
	var mu sync.Mutex
	for _, root := range roots {
		root := root
		pkg := root.GetName()
		for _, svc := range root.GetService() {
			grp.Go(func() error {
				destFn := filepath.Join(
					destDir,
					strings.TrimSuffix(filepath.Base(pkg), ".proto")+".grpcer.go",
				)
				content, err := genGo(destPkg, pkg, svc, root.GetDependency())
				mu.Lock()
				resp.File = append(resp.File, &protoc.CodeGeneratorResponse_File{
					Name:    &destFn,
					Content: &content,
				})
				mu.Unlock()
				return err
			})
		}
	}

	if err := grp.Wait(); err != nil {
		errS := err.Error()
		resp.Error = &errS
	}
	return nil
}

var goTmpl = template.Must(template.
	New("go").
	Funcs(template.FuncMap{
		"trimLeft":    strings.TrimLeft,
		"trimLeftDot": func(s string) string { return strings.TrimLeft(s, ".") },
		"base": func(s string) string {
			if i := strings.LastIndexByte(s, '.'); i >= 0 {
				return s[i+1:]
			}
			return s
		},
		"now": func(patterns ...string) string {
			pattern := time.RFC3339
			if len(patterns) > 0 {
				pattern = patterns[0]
			}
			return time.Now().Format(pattern)
		},
	}).
	Parse(`// Generated with protoc-gen-grpcer
//	from "{{.ProtoFile}}"
//	at   {{now}}
//
// DO NOT EDIT!

package {{.Package}}

import (
	"io"
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	errors "github.com/pkg/errors"
	gprcer "github.com/UNO-SOFT/gprcer"

	pb "{{.Import}}"
	{{range .Dependencies}}"{{.}}"
	{{end}}
)

type client struct {
	pb.{{.GetName}}Client
}
func NewClient(cc *grpc.ClientConn) grpcer.Client {
	return client{ pb.New{{.GetName}}Client(cc) }
}

type onceRecv struct {
	Out interface{}
	done bool
}
func (o *onceRecv) Recv() (interface{}, error) {
	if o.done {
		return nil, io.EOF
	}
	out := o.Out
	o.done, o.Out = true, nil
	return out, nil
}

type multiRecv func() (interface{}, error)
func (m multiRecv) Recv() (interface{}, error) {
	return m()
}

func (c client) Input(name string) interface{} {
	switch name {
	{{range .GetMethod}}case "{{.GetName}}": return {{trimLeftDot .GetInputType }}{}
	{{end}}
	}
	return nil
}

func (c client) Call(name string, ctx context.Context, in interface{}, opts ...grpc.CallOption) (grpcer.Receiver, error) {
	switch name {
	{{range .GetMethod}}case "{{.GetName}}":
		input := in.(*{{trimLeftDot .GetInputType}})
		{{if .GetServerStreaming -}}
		recv, err := c.{{.Name}}(ctx, input, opts...)
		if err != nil {
			return nil, err
		}
		return multiRecv(func() (interface{}, error) { return recv.Recv() }), nil
		{{else -}}
		out, err := c.{{.Name}}(ctx, input, opts...)
		return &onceRecv{Out:out}, err
		{{end}}
	{{end}}
	}
	return nil, errors.Errorf("name %q not found", name)
}



`))

func genGo(destPkg, protoFn string, svc *descriptor.ServiceDescriptorProto, dependencies []string) (string, error) {
	if destPkg == "" {
		destPkg = "main"
	}
	needed := make(map[string]struct{}, len(dependencies))
	for _, m := range svc.GetMethod() {
		//for _, t := range []string{m.GetInputType(), m.GetOutputType()} {
		t := m.GetInputType()
		if !strings.HasPrefix(t, ".") {
			continue
		}
		t = t[1:]
		needed[strings.SplitN(t, ".", 2)[0]] = struct{}{}
	}
	deps := make([]string, 0, len(dependencies))
	for _, dep := range dependencies {
		k := filepath.Dir(dep)
		if _, ok := needed[filepath.Base(k)]; !ok {
			continue
		}
		deps = append(deps, k)
	}
	var buf bytes.Buffer
	err := goTmpl.Execute(&buf, struct {
		ProtoFile, Package, Import string
		Dependencies               []string
		*descriptor.ServiceDescriptorProto
	}{
		ProtoFile:              protoFn,
		Package:                destPkg,
		Import:                 filepath.Dir(protoFn),
		Dependencies:           deps,
		ServiceDescriptorProto: svc,
	})
	return buf.String(), err
}
