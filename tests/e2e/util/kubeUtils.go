package util

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"text/template"

	"github.com/golang/glog"
)

func Fill(outFile, inFile string, values interface{}) error {
	var bytes bytes.Buffer
	w := bufio.NewWriter(&bytes)

	tmpl, err := template.ParseFiles(GetTestRuntimePath() + "/tests/e2e/framework/testdata/" + inFile)
	if err != nil {
		return err
	}

	if err := tmpl.Execute(w, values); err != nil {
		return err
	}

	if err := w.Flush(); err != nil {
		return err
	}

	if err := ioutil.WriteFile(outFile, bytes.Bytes(), 0644); err != nil {
		return err
	}

	return nil
}

func CreateNamespace(n string) error {
	if _, err := Shell(fmt.Sprintf("kubectl create namespace %s", n)); err != nil {
		return err
	}
	glog.Infof("nameSpace %s created\n", n)
	return nil
}

func DeleteNamespace(n string) error {
	_, err := Shell(fmt.Sprintf("kubectl delete namespace %s", n))
	return err
}

func KubeApply(n, yaml string) error {
	_, err := Shell(fmt.Sprintf("kubectl apply -n %s -f %s", n, yaml))
	return err
}
