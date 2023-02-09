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

package ambient

import (
	"reflect"
	"testing"
)

func TestDeleteFrom(t *testing.T) {
	indexer := map[string][]Workload{
		"1": []Workload{{
			UID: "uid1",
		}},
		"2": []Workload{
			{
				UID: "uid2",
			},
			{
				UID: "uid3",
			},
			{
				UID: "uid4",
			},
			{
				UID: "uid5",
			},
		},
	}

	type args struct {
		m map[string][]Workload
		k string
		w Workload
	}
	tests := []struct {
		name string
		args args
		want map[string][]Workload
	}{
		{
			name: "delete with only one workload",
			args: args{
				m: indexer,
				k: "1",
				w: Workload{
					UID: "uid1",
				},
			},
			want: map[string][]Workload{
				"2": []Workload{
					{
						UID: "uid2",
					},
					{
						UID: "uid3",
					},
					{
						UID: "uid4",
					},
					{
						UID: "uid5",
					},
				},
			},
		},
		{
			name: "delete first workload",
			args: args{
				m: indexer,
				k: "2",
				w: Workload{
					UID: "uid2",
				},
			},
			want: map[string][]Workload{
				"2": []Workload{
					{
						UID: "uid3",
					},
					{
						UID: "uid4",
					},
					{
						UID: "uid5",
					},
				},
			},
		},
		{
			name: "delete mid workload",
			args: args{
				m: indexer,
				k: "2",
				w: Workload{
					UID: "uid4",
				},
			},
			want: map[string][]Workload{
				"2": []Workload{
					{
						UID: "uid3",
					},
					{
						UID: "uid5",
					},
				},
			},
		},
		{
			name: "delete last workload",
			args: args{
				m: indexer,
				k: "2",
				w: Workload{
					UID: "uid5",
				},
			},
			want: map[string][]Workload{
				"2": []Workload{
					{
						UID: "uid3",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleteFrom(tt.args.m, tt.args.k, tt.args.w)
			if !reflect.DeepEqual(tt.args.m, tt.want) {
				t.Errorf("deleteFromMap() = %v, want %v", tt.args.m, tt.want)
			}
		})
	}
}
