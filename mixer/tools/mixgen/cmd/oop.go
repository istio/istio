package cmd

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/mixer/pkg/protobuf/descriptor"
	"istio.io/istio/mixer/tools/codegen/pkg/modelgen"
)

type (
	// ServerGenerator generates server code for out of process adapter.
	ServerGenerator struct {
		OutCmdDir            string
		OutHelmDir           string
		AdapterName          string
		AdapterPackage       string
		AdapterConfigPackage string
		TemplatePaths        []string
		TemplatePackages     []string
		Models               []*modelgen.Model
	}

	fillTemplateFunc func() ([]byte, error)
)

func serverGenCmd(rawArgs []string, printf, fatalf shared.FormatFn) *cobra.Command {
	var outServerFile string
	var adapterName string
	var templateFiles []string
	var adapterPackage string
	var configPackage string
	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "creates server code from the given templates for an out of process adapter",
		Run: func(cmd *cobra.Command, args []string) {
			var err error
			outServerFile, err = filepath.Abs(outServerFile)
			if err != nil {
				fatalf("Invalid path %s: %v", outServerFile, err)
			}

			generator := ServerGenerator{
				OutCmdDir:            outServerFile + "/cmd",
				OutHelmDir:           outServerFile + "/helm",
				AdapterName:          adapterName,
				AdapterPackage:       adapterPackage,
				AdapterConfigPackage: configPackage,
				TemplatePaths:        templateFiles,
				TemplatePackages:     make([]string, 0, len(templateFiles)),
				Models:               make([]*modelgen.Model, 0, len(templateFiles)),
			}
			for _, t := range generator.TemplatePaths {
				fds, err := getFileDescSet(t)
				if err != nil {
					fatalf("cannot parse file '%s' as a FileDescriptorSetProto: %v", t, err)
				}

				parser := descriptor.CreateFileDescriptorSetParser(fds, map[string]string{}, "")
				model, err := modelgen.Create(parser)
				if err != nil {
					fatalf("cannot create model for '%s': %v", t, err)
				}
				generator.Models = append(generator.Models, model)
				generator.TemplatePackages = append(generator.TemplatePackages, filepath.Dir(stripGoPath(t)))
			}
			if err := generator.Generate(); err != nil {
				fatalf("%v", err)
			}
		},
	}
	serverCmd.PersistentFlags().StringArrayVarP(&templateFiles, "templates", "t", nil,
		"paths to the descriptor files for all the templates that the adapter supports.")
	serverCmd.PersistentFlags().StringVar(&adapterName, "adapter_name", "",
		"name of the adapter.")
	serverCmd.PersistentFlags().StringVar(&outServerFile, "out_dir", "./",
		"output directory for out of process adapter server code.")
	serverCmd.PersistentFlags().StringVar(&adapterPackage, "adapter_package", "",
		"adapter package, e.g. istio.io/mixer/adapter/prometheus")
	serverCmd.PersistentFlags().StringVar(&configPackage, "config_package", "",
		"adapter config package, e.g. istio.io/mixer/adapter/prometheus/config")
	return serverCmd
}

func (sg *ServerGenerator) generateFile(filePath string, ft fillTemplateFunc, isGo bool) error {
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	fd, err := ft()
	if err != nil {
		return err
	}
	if isGo {
		fd, err = format.Source(fd)
		if err != nil {
			return err
		}
	}
	if _, err = f.Write(fd); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return err
	}
	return nil
}

// Generate generates server and main go code for out of process adapter.
func (sg *ServerGenerator) Generate() error {
	if _, err := os.Stat(sg.OutCmdDir + "/server"); os.IsNotExist(err) {
		err = os.MkdirAll(sg.OutCmdDir+"/server", 0755)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	if _, err := os.Stat(sg.OutHelmDir + "/templates"); os.IsNotExist(err) {
		err = os.MkdirAll(sg.OutHelmDir+"/templates", 0755)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	genParams := []struct {
		path string
		fn   fillTemplateFunc
		isGo bool
	}{
		{sg.OutCmdDir + "/server/nosession.go", sg.getNoSessionServer, true},
		{sg.OutCmdDir + "/main.go", sg.getMainGoContent, true},
		{sg.OutCmdDir + "/Makefile", sg.getMakeFileContent, false},
		{sg.OutCmdDir + "/Dockerfile", sg.getDockerFileContent, false},
		{sg.OutHelmDir + "/Chart.yaml", sg.getChartFileContent, false},
		{sg.OutHelmDir + "/values.yaml", sg.getChartValueFileContent, false},
		{sg.OutHelmDir + "/templates/service.yaml", sg.getServiceYamlFileContent, false},
		{sg.OutHelmDir + "/templates/deployment.yaml", sg.getDeploymentYamlFileContent, false},
	}

	for _, param := range genParams {
		if err := sg.generateFile(param.path, param.fn, param.isGo); err != nil {
			return err
		}
	}
	return nil
}

func (sg *ServerGenerator) getNoSessionServer() ([]byte, error) {
	importProto := false
	serverTmpl, err := template.New("ProcServer").Funcs(
		template.FuncMap{
			"Capitalize": strings.Title,
			"FindPackage": func(in modelgen.FieldInfo) string {
				for _, m := range sg.Models {
					for _, r := range m.ResourceMessages {
						for _, f := range r.Fields {
							if reflect.DeepEqual(in, f) {
								return m.GoPackageName
							}
						}
					}
					for _, f := range m.TemplateMessage.Fields {
						if reflect.DeepEqual(in, f) {
							return m.GoPackageName
						}
					}
				}
				return ""
			},
			"TrimGoType": func(goType string) string {
				if strings.Contains(goType, ".") {
					parts := strings.Split(goType, ".")
					goType = parts[len(parts)-1]
				}
				return strings.Trim(goType, "[]*")
			},
			"AddProtoToImpt": func() string {
				importProto = true
				return ""
			},
		}).Parse(noSessionServerTempl)
	if err != nil {
		return nil, fmt.Errorf("cannot load template: %v", err)
	}
	serverBuf := new(bytes.Buffer)
	err = serverTmpl.Execute(serverBuf, sg)
	if err != nil {
		return nil, fmt.Errorf("cannot execute the template with the given data: %v", err)
	}
	var retBytes []byte
	if importProto {
		retBytes = bytes.Replace(serverBuf.Bytes(), []byte("$$additional_imports$$"), []byte("proto \"github.com/gogo/protobuf/types\""), 1)
	} else {
		retBytes = bytes.Replace(serverBuf.Bytes(), []byte("$$additional_imports$$"), []byte(""), 1)
	}
	return retBytes, nil
}

func (sg *ServerGenerator) getMainGoContent() ([]byte, error) {
	type cmdMain struct {
		AdapterName string
		PackagePath string
	}
	cm := cmdMain{AdapterName: sg.AdapterName, PackagePath: stripGoPath(sg.OutCmdDir)}
	mainTmpl, err := template.New("ProcMain").Funcs(
		template.FuncMap{
			"Capitalize": strings.Title,
		}).Parse(oopMainTempl)
	mainBuf := new(bytes.Buffer)
	err = mainTmpl.Execute(mainBuf, cm)
	if err != nil {
		return nil, fmt.Errorf("cannot execute the template with the given data: %v", err)
	}
	return mainBuf.Bytes(), nil
}

func (sg *ServerGenerator) getMakeFileContent() ([]byte, error) {
	type cmdMake struct {
		AdapterName string
	}
	cm := cmdMake{AdapterName: sg.AdapterName}
	makeTmpl, err := template.New("ProcMake").Parse(makeFileTmpl)
	makeBuf := new(bytes.Buffer)
	err = makeTmpl.Execute(makeBuf, cm)
	if err != nil {
		return nil, fmt.Errorf("cannot execute the template with the given data: %v", err)
	}
	return makeBuf.Bytes(), nil
}

func (sg *ServerGenerator) getDockerFileContent() ([]byte, error) {
	type cmdDocker struct {
		AdapterName string
	}
	cm := cmdDocker{AdapterName: sg.AdapterName}
	dockerTmpl, err := template.New("ProcDocker").Parse(dockerFileTmpl)
	dockerBuf := new(bytes.Buffer)
	err = dockerTmpl.Execute(dockerBuf, cm)
	if err != nil {
		return nil, fmt.Errorf("cannot execute the template with the given data: %v", err)
	}
	return dockerBuf.Bytes(), nil
}

func (sg *ServerGenerator) getChartFileContent() ([]byte, error) {
	type cmdChart struct {
		AdapterName string
	}
	cm := cmdChart{AdapterName: sg.AdapterName}
	chartTmpl, err := template.New("ProcChart").Parse(helmChartTmpl)
	chartBuf := new(bytes.Buffer)
	err = chartTmpl.Execute(chartBuf, cm)
	if err != nil {
		return nil, fmt.Errorf("cannot execute the template with the given data: %v", err)
	}
	return chartBuf.Bytes(), nil
}

func (sg *ServerGenerator) getChartValueFileContent() ([]byte, error) {
	type cmdChartValue struct {
		AdapterName string
	}
	cm := cmdChartValue{AdapterName: sg.AdapterName}
	chartValueTmpl, err := template.New("ProcChartValue").Parse(helmValueTemp)
	chartValueBuf := new(bytes.Buffer)
	err = chartValueTmpl.Execute(chartValueBuf, cm)
	if err != nil {
		return nil, fmt.Errorf("cannot execute the template with the given data: %v", err)
	}
	return chartValueBuf.Bytes(), nil
}

func (sg *ServerGenerator) getServiceYamlFileContent() ([]byte, error) {
	type cmdServiceYaml struct {
		AdapterName string
	}
	cm := cmdServiceYaml{AdapterName: sg.AdapterName}
	serviceYamlTmpl, err := template.New("ProcService").Parse(helmServiceTmpl)
	serviceYamlBuf := new(bytes.Buffer)
	err = serviceYamlTmpl.Execute(serviceYamlBuf, cm)
	if err != nil {
		return nil, fmt.Errorf("cannot execute the template with the given data: %v", err)
	}
	return serviceYamlBuf.Bytes(), nil
}

func (sg *ServerGenerator) getDeploymentYamlFileContent() ([]byte, error) {
	type cmdDeploymentYaml struct {
		AdapterName string
	}
	cm := cmdDeploymentYaml{AdapterName: sg.AdapterName}
	deploymentYamlTmpl, err := template.New("ProcDeployment").Parse(helmDeploymentTmpl)
	deploymentYamlBuf := new(bytes.Buffer)
	err = deploymentYamlTmpl.Execute(deploymentYamlBuf, cm)
	if err != nil {
		return nil, fmt.Errorf("cannot execute the template with the given data: %v", err)
	}
	return deploymentYamlBuf.Bytes(), nil
}

func stripGoPath(path string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	goPath := os.Getenv("GOPATH") + "/src/"
	if strings.HasPrefix(absPath, goPath) {
		return strings.TrimPrefix(absPath, goPath)
	}
	return ""
}
