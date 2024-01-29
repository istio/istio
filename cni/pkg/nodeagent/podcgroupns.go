// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nodeagent

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"unicode"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/util/sets"
)

type PodToNetns map[types.UID]NetnsCloser

func (p PodToNetns) Close() {
	for _, netns := range p {
		netns.Close()
	}
}

type PodNetnsFinder interface {
	FindNetnsForPods(filter sets.Set[types.UID]) (PodToNetns, error)
}

type PodNetnsProcFinder struct {
	proc fs.FS
}

func NewPodNetnsProcFinder(proc fs.FS) *PodNetnsProcFinder {
	return &PodNetnsProcFinder{proc: proc}
}

func isNotNumber(r rune) bool {
	return r < '0' || r > '9'
}

func (p *PodNetnsProcFinder) FindNetnsForPods(filter sets.Set[types.UID]) (PodToNetns, error) {
	/*
		for each process, find its netns inode,
		if we already seen the inode, skip it
		if we haven't seen the inode, check the process cgroup and see if we
		can extract a pod uid from it.
		if we can, open the netns, and save a map of uid->netns-fd
	*/

	podUIDNetns := make(PodToNetns)

	if filter != nil && filter.Len() == 0 {
		return podUIDNetns, nil
	}
	netnsObserved := sets.New[uint64]()

	entries, err := fs.ReadDir(p.proc, ".")
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		// we can't break here because we need to close all the netns we opened
		// plus we want to return whatever we can to the user.
		res, err := p.processEntry(p.proc, netnsObserved, filter, entry)
		if err != nil {
			log.Debugf("error processing entry: %s %v", entry.Name(), err)
			continue
		}
		if res == nil {
			continue
		}
		podUIDNetns[res.uid] = &NetnsWithFd{
			netns: res.netns,
			fd:    res.netnsfd,
			inode: res.inode,
		}

	}
	return podUIDNetns, nil
}

type PodNetnsEntry struct {
	uid     types.UID
	netns   fs.File
	netnsfd uintptr
	inode   uint64
}

func (p *PodNetnsProcFinder) processEntry(proc fs.FS, netnsObserved sets.Set[uint64], filter sets.Set[types.UID], entry fs.DirEntry) (*PodNetnsEntry, error) {
	if !isProcess(entry) {
		return nil, nil
	}

	netnsName := path.Join(entry.Name(), "ns", "net")
	fi, err := fs.Stat(proc, netnsName)
	if err != nil {
		return nil, err
	}

	inode, err := GetInode(fi)
	if err != nil {
		return nil, err
	}
	if _, ok := netnsObserved[inode]; ok {
		log.Debugf("netns: %d already processed. skipping", inode)
		return nil, nil
	}

	cgroup, err := proc.Open(path.Join(entry.Name(), "cgroup"))
	if err != nil {
		return nil, nil
	}
	defer cgroup.Close()

	var cgroupData bytes.Buffer
	_, err = io.Copy(&cgroupData, cgroup)
	if err != nil {
		return nil, nil
	}

	uid, _, err := GetPodUIDAndContainerID(cgroupData)
	if err != nil {
		return nil, err
	}
	if filter != nil && !filter.Contains(uid) {
		return nil, nil
	}

	netns, err := proc.Open(netnsName)
	if err != nil {
		return nil, err
	}
	fd, err := GetFd(netns)
	if err != nil {
		netns.Close()
		return nil, err
	}
	netnsObserved[inode] = struct{}{}
	log.Debugf("found pod to netns: %s %d", uid, inode)

	return &PodNetnsEntry{
		uid:     uid,
		netns:   netns,
		netnsfd: fd,
		inode:   inode,
	}, nil
}

func isProcess(entry fs.DirEntry) bool {
	// check if it is a directory
	if !entry.IsDir() {
		return false
	}

	// check if it is a number
	if strings.IndexFunc(entry.Name(), isNotNumber) != -1 {
		return false
	}
	return true
}

func GetFd(f fs.File) (uintptr, error) {
	if fdable, ok := f.(interface{ Fd() uintptr }); ok {
		return fdable.Fd(), nil
	}

	return 0, fmt.Errorf("unable to get fd")
}

/// mostly copy pasted from spire below:

// regexes listed here have to exclusively match a cgroup path
// the regexes must include two named groups "poduid" and "containerid"
// if the regex needs to exclude certain substrings, the "mustnotmatch" group can be used
// nolint: lll
var cgroupREs = []*regexp.Regexp{
	// the regex used to parse out the pod UID and container ID from a
	// cgroup name. It assumes that any ".scope" suffix has been trimmed off
	// beforehand.  CAUTION: we used to verify that the pod and container id were
	// descendants of a kubepods directory, however, as of Kubernetes 1.21, cgroups
	// namespaces are in use and therefore we can no longer discern if that is the
	// case from within SPIRE agent container (since the container itself is
	// namespaced). As such, the regex has been relaxed to simply find the pod UID
	// followed by the container ID with allowances for arbitrary punctuation, and
	// container runtime prefixes, etc.
	regexp.MustCompile(`` +
		// "pod"-prefixed Pod UID (with punctuation separated groups) followed by punctuation
		`[[:punct:]]pod(?P<poduid>[[:xdigit:]]{8}[[:punct:]]?[[:xdigit:]]{4}[[:punct:]]?[[:xdigit:]]{4}[[:punct:]]?[[:xdigit:]]{4}[[:punct:]]?[[:xdigit:]]{12})[[:punct:]]` +
		// zero or more punctuation separated "segments" (e.g. "docker-")
		`(?:[[:^punct:]]+[[:punct:]])*` +
		// non-punctuation end of string, i.e., the container ID
		`(?P<containerid>[[:^punct:]]+)$`),

	// This regex applies for container runtimes, that won't put the PodUID into
	// the cgroup name.
	// Currently only cri-o in combination with kubeedge is known for this abnormally.
	regexp.MustCompile(`` +
		// intentionally empty poduid group
		`(?P<poduid>)` +
		// mustnotmatch group: cgroup path must not include a poduid
		`(?P<mustnotmatch>pod[[:xdigit:]]{8}[[:punct:]]?[[:xdigit:]]{4}[[:punct:]]?[[:xdigit:]]{4}[[:punct:]]?[[:xdigit:]]{4}[[:punct:]]?[[:xdigit:]]{12}[[:punct:]])?` +
		// /crio-
		`(?:[[:^punct:]]*/*)*crio[[:punct:]]` +
		// non-punctuation end of string, i.e., the container ID
		`(?P<containerid>[[:^punct:]]+)$`),
}

func reSubMatchMap(r *regexp.Regexp, str string) map[string]string {
	match := r.FindStringSubmatch(str)
	if match == nil {
		return nil
	}
	subMatchMap := make(map[string]string)
	for i, name := range r.SubexpNames() {
		if i != 0 {
			subMatchMap[name] = match[i]
		}
	}
	return subMatchMap
}

func isValidCGroupPathMatches(matches map[string]string) bool {
	if matches == nil {
		return false
	}
	if matches["mustnotmatch"] != "" {
		return false
	}
	return true
}

// nolint: lll
func getPodUIDAndContainerIDFromCGroupPath(cgroupPath string) (types.UID, string, bool) {
	// We are only interested in kube pods entries, for example:
	// - /kubepods/burstable/pod2c48913c-b29f-11e7-9350-020968147796/9bca8d63d5fa610783847915bcff0ecac1273e5b4bed3f6fa1b07350e0135961
	// - /docker/8d461fa5765781bcf5f7eb192f101bc3103d4b932e26236f43feecfa20664f96/kubepods/besteffort/poddaa5c7ee-3484-4533-af39-3591564fd03e/aff34703e5e1f89443e9a1bffcc80f43f74d4808a2dd22c8f88c08547b323934
	// - /kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod2c48913c-b29f-11e7-9350-020968147796.slice/docker-9bca8d63d5fa610783847915bcff0ecac1273e5b4bed3f6fa1b07350e0135961.scope
	// - /kubepods-besteffort-pod72f7f152_440c_66ac_9084_e0fc1d8a910c.slice:cri-containerd:b2a102854b4969b2ce98dc329c86b4fb2b06e4ad2cc8da9d8a7578c9cd2004a2"
	// - /../../pod2c48913c-b29f-11e7-9350-020968147796/9bca8d63d5fa610783847915bcff0ecac1273e5b4bed3f6fa1b07350e0135961
	// - 0::/../crio-45490e76e0878aaa4d9808f7d2eefba37f093c3efbba9838b6d8ab804d9bd814.scope
	// First trim off any .scope suffix. This allows for a cleaner regex since
	// we don't have to muck with greediness. TrimSuffix is no-copy so this
	// is cheap.
	cgroupPath = strings.TrimSuffix(cgroupPath, ".scope")

	var matchResults map[string]string
	for _, regex := range cgroupREs {
		matches := reSubMatchMap(regex, cgroupPath)
		if isValidCGroupPathMatches(matches) {
			if matchResults != nil {
				return "", "", false
			}
			matchResults = matches
		}
	}

	if matchResults != nil {
		var podUID types.UID
		if matchResults["poduid"] != "" {
			podUID = canonicalizePodUID(matchResults["poduid"])
		}
		return podUID, matchResults["containerid"], true
	}
	return "", "", false
}

// canonicalizePodUID converts a Pod UID, as represented in a cgroup path, into
// a canonical form. Practically this means that we convert any punctuation to
// dashes, which is how the UID is represented within Kubernetes.
func canonicalizePodUID(uid string) types.UID {
	return types.UID(strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) {
			r = '-'
		}
		return r
	}, uid))
}

// Cgroup represents a linux cgroup.
type Cgroup struct {
	HierarchyID    string
	ControllerList string
	GroupPath      string
}

// GetCGroups returns a slice of cgroups for pid using fs for filesystem calls.
//
// The expected cgroup format is "hierarchy-ID:controller-list:cgroup-path", and
// this function will return an error if every cgroup does not meet that format.
//
// For more information, see:
//   - http://man7.org/linux/man-pages/man7/cgroups.7.html
//   - https://www.kernel.org/doc/Documentation/cgroup-v2.txt
func GetCgroups(procCgroupData bytes.Buffer) ([]Cgroup, error) {
	reader := bytes.NewReader(procCgroupData.Bytes())
	var cgroups []Cgroup
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		token := scanner.Text()
		substrings := strings.SplitN(token, ":", 3)
		if len(substrings) < 3 {
			return nil, fmt.Errorf("cgroup entry contains %v colons, but expected at least 2 colons: %q", len(substrings), token)
		}
		cgroups = append(cgroups, Cgroup{
			HierarchyID:    substrings[0],
			ControllerList: substrings[1],
			GroupPath:      substrings[2],
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return cgroups, nil
}

func GetPodUIDAndContainerID(procCgroupData bytes.Buffer) (types.UID, string, error) {
	cgroups, err := GetCgroups(procCgroupData)
	if err != nil {
		return "", "", fmt.Errorf("unable to obtain cgroups: %v", err)
	}

	return getPodUIDAndContainerIDFromCGroups(cgroups)
}

func getPodUIDAndContainerIDFromCGroups(cgroups []Cgroup) (types.UID, string, error) {
	var podUID types.UID
	var containerID string
	for _, cgroup := range cgroups {
		candidatePodUID, candidateContainerID, ok := getPodUIDAndContainerIDFromCGroupPath(cgroup.GroupPath)
		switch {
		case !ok:
			// Cgroup did not contain a container ID.
			continue
		case containerID == "":
			// This is the first container ID found so far.
			podUID = candidatePodUID
			containerID = candidateContainerID
		case containerID != candidateContainerID:
			// More than one container ID found in the cgroups.
			return "", "", fmt.Errorf("multiple container IDs found in cgroups (%s, %s)",
				containerID, candidateContainerID)
		case podUID != candidatePodUID:
			// More than one pod UID found in the cgroups.
			return "", "", fmt.Errorf("multiple pod UIDs found in cgroups (%s, %s)",
				podUID, candidatePodUID)
		}
	}

	return podUID, containerID, nil
}
