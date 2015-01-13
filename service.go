package main

import (
	"github.com/aerospike-labs/minion/service"

	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

// ----------------------------------------------------------------------------
//
// Types
//
// ----------------------------------------------------------------------------

type ServiceContext struct {
	SendEventMessage func(data, event, id string)
	Registry         map[string]string
}

type ServiceInstall struct {
	Id     string                 `json:"id"`
	URL    string                 `json:"url"`
	Params map[string]interface{} `json:"params"`
}

// ----------------------------------------------------------------------------
//
// Bundles Methods
//
// ----------------------------------------------------------------------------

func (self *ServiceContext) getenv(serviceName string, serviceUrl string) []string {

	svcPath := filepath.Join(rootPath, "svc", serviceName)
	goRoot := filepath.Join(rootPath, "go")
	goBin := filepath.Join(goRoot, "bin")

	env := []string{}
	env = append(env, "GOPATH="+svcPath)
	env = append(env, "GOROOT="+goRoot)
	env = append(env, "PATH="+os.Getenv("PATH")+":"+goBin)
	env = append(env, "SERVICE_NAME="+serviceName)
	env = append(env, "SERVICE_URL="+serviceUrl)
	env = append(env, "SERVICE_PATH="+svcPath)
	return env
}

// List Bundles
func (self *ServiceContext) List(req *http.Request, args *struct{}, res *map[string]string) error {
	*res = self.Registry
	return nil
}

// Install a Bundle
func (self *ServiceContext) Install(req *http.Request, svc *ServiceInstall, res *string) error {

	var err error = nil

	serviceUrl, exists := self.Registry[svc.Id]
	if exists {
		return service.Exists
	}

	// make sure the svc path exists
	svcPath := filepath.Join(rootPath, "svc", svc.Id)
	os.MkdirAll(svcPath, 0755)

	// env
	env := self.getenv(svc.Id, serviceUrl)

	// download the service
	get := exec.Command("go", "get", "-u", svc.URL)
	get.Env = env
	get.Dir = svcPath
	getOut, err := get.CombinedOutput()
	println("out: ", string(getOut))
	if err != nil {
		println("error: ", err.Error())
		return err
	}

	binPath := filepath.Join("bin", svc.Id)
	build := exec.Command("go", "build", "-o", binPath, svc.URL)
	build.Env = env
	build.Dir = svcPath
	buildOut, err := build.CombinedOutput()
	println("out: ", string(buildOut))
	if err != nil {
		println("error: ", err.Error())
		return err
	}

	self.Registry[svc.Id] = svc.URL

	// run "install" command
	if err = self.run(svc.Id, "install", svc.Params, res); err != nil {
		return err
	}

	// *res = string(out)
	return err
}

// Remove a Bundle
func (self *ServiceContext) Remove(req *http.Request, serviceId *string, res *string) error {

	var err error = nil

	serviceURL, exists := self.Registry[*serviceId]
	if !exists {
		return service.NotFound
	}

	delete(self.Registry, *serviceId)

	// run "remove" command
	if err = self.run(*serviceId, "remove", map[string]interface{}{}, res); err != nil {
		return err
	}

	// clean up

	cmd := exec.Command("go", "clean", serviceURL)
	cmd.Env = self.getenv(*serviceId, serviceURL)
	out, err := cmd.CombinedOutput()
	println("out: ", string(out))
	if err != nil {
		println("error: ", err.Error())
		return err
	}

	srcPath := filepath.Join(rootPath, "src", serviceURL)
	if err = os.RemoveAll(srcPath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	}

	binPath := filepath.Join(rootPath, "bin", *serviceId)
	if err = os.RemoveAll(binPath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	}

	svcPath := filepath.Join(rootPath, "svc", *serviceId)
	if err = os.RemoveAll(svcPath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	}

	return err
}

// Check Existence of a Service
func (self *ServiceContext) Exists(req *http.Request, serviceId *string, res *bool) error {

	if _, exists := self.Registry[*serviceId]; exists {
		*res = true
	} else {
		*res = false
	}

	return nil
}

// Status of the Service
func (self *ServiceContext) Status(req *http.Request, serviceName *string, res *string) error {
	return self.run(*serviceName, "status", map[string]interface{}{}, res)
}

// Start the Service
func (self *ServiceContext) Start(req *http.Request, serviceName *string, res *string) error {
	return self.run(*serviceName, "start", map[string]interface{}{}, res)
}

// Stop the Service
func (self *ServiceContext) Stop(req *http.Request, serviceName *string, res *string) error {
	return self.run(*serviceName, "stop", map[string]interface{}{}, res)
}

// Stats of the Service
func (self *ServiceContext) Stats(req *http.Request, serviceName *string, res *map[string]int) error {
	var out string = ""

	err := self.run(*serviceName, "stats", map[string]interface{}{}, &out)
	if err != nil {
		return err
	}

	err = json.Unmarshal([]byte(out), res)
	return err
}

// Run a Service Command
func (self *ServiceContext) run(serviceId string, commandName string, params map[string]interface{}, res *string) error {
	var err error = nil

	serviceUrl, serviceExists := self.Registry[serviceId]
	if !serviceExists {
		return service.NotFound
	}

	svcPath := filepath.Join(rootPath, "svc", serviceId)
	binPath := filepath.Join(svcPath, "bin", serviceId)
	cmd := exec.Command(binPath, commandName)
	cmd.Dir = svcPath
	cmd.Env = self.getenv(serviceId, serviceUrl)

	b, err := json.Marshal(params)
	if err != nil {
		return err
	} else {
		cmd.Stdin = bytes.NewReader(b)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	*res = string(out)
	return err
}
