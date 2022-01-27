//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package locality

type Match func(Instance) bool

func MatchZone(l Instance) Match {
	return func(o Instance) bool {
		return l.Zone == o.Zone &&
			l.Region == o.Region
	}
}

func MatchRegion(l Instance) Match {
	return func(o Instance) bool {
		return l.Region == o.Region
	}
}

func MatchOtherZoneInSameRegion(l Instance) Match {
	return And(MatchRegion(l), Not(MatchZone(l)))
}

func And(m1 Match, m2 Match) Match {
	return func(o Instance) bool {
		return m1(o) && m2(o)
	}
}

func Not(match Match) Match {
	return func(o Instance) bool {
		return !match(o)
	}
}
