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

package dpop

import (
	"fmt"
	"time"
)

// DPoPSettings contains DPoP configuration for RequestAuthentication
type DPoPSettings struct {
	// Enabled indicates whether DPoP validation is enabled
	Enabled bool `protobuf:"varint,1,opt,name=enabled,proto3" json:"enabled,omitempty"`
	
	// Required indicates whether DPoP proofs are required for requests
	// If false, requests without DPoP will be allowed (optional DPoP)
	Required bool `protobuf:"varint,2,opt,name=required,proto3" json:"required,omitempty"`
	
	// MaxAge defines the maximum age for DPoP proofs
	// If not specified, defaults to 5 minutes
	MaxAge *time.Duration `protobuf:"bytes,3,opt,name=max_age,json=maxAge,proto3,stdduration" json:"max_age,omitempty"`
	
	// ReplayCacheSize defines the maximum size of the replay cache
	// If not specified, defaults to 10000
	ReplayCacheSize int32 `protobuf:"varint,4,opt,name=replay_cache_size,json=replayCacheSize,proto3" json:"replay_cache_size,omitempty"`
	
	// CacheCleanupInterval defines how often to clean the replay cache
	// If not specified, defaults to 10 minutes
	CacheCleanupInterval *time.Duration `protobuf:"bytes,5,opt,name=cache_cleanup_interval,json=cacheCleanupInterval,proto3,stdduration" json:"cache_cleanup_interval,omitempty"`
}

// DefaultDPoPSettings returns default DPoP settings
func DefaultDPoPSettings() *DPoPSettings {
	return &DPoPSettings{
		Enabled:              false,
		Required:             false,
		MaxAge:               durationPtr(DefaultDPoPMaxAge),
		ReplayCacheSize:      10000,
		CacheCleanupInterval: durationPtr(DefaultCacheCleanupInterval),
	}
}

// ToConfig converts DPoPSettings to DPoPConfig
func (s *DPoPSettings) ToConfig() *DPoPConfig {
	config := DefaultDPoPConfig()
	
	if s.MaxAge != nil {
		config.MaxAge = *s.MaxAge
	}
	
	if s.ReplayCacheSize > 0 {
		config.ReplayCacheSize = int(s.ReplayCacheSize)
	}
	
	if s.CacheCleanupInterval != nil {
		config.CacheCleanupInterval = *s.CacheCleanupInterval
	}
	
	return config
}

// durationPtr returns a pointer to the given duration
func durationPtr(d time.Duration) *time.Duration {
	return &d
}

// IsEnabled returns true if DPoP is enabled
func (s *DPoPSettings) IsEnabled() bool {
	return s != nil && s.Enabled
}

// IsRequired returns true if DPoP is required
func (s *DPoPSettings) IsRequired() bool {
	return s.IsEnabled() && s.Required
}

// Validate validates the DPoP settings
func (s *DPoPSettings) Validate() error {
	if s == nil {
		return nil
	}
	
	if s.MaxAge != nil && *s.MaxAge <= 0 {
		return fmt.Errorf("DPoP max_age must be positive")
	}
	
	if s.MaxAge != nil && *s.MaxAge > time.Hour {
		return fmt.Errorf("DPoP max_age cannot exceed 1 hour for security")
	}
	
	if s.ReplayCacheSize < 0 {
		return fmt.Errorf("DPoP replay_cache_size cannot be negative")
	}
	
	if s.CacheCleanupInterval != nil && *s.CacheCleanupInterval <= 0 {
		return fmt.Errorf("DPoP cache_cleanup_interval must be positive")
	}
	
	return nil
}

// Merge merges other DPoPSettings into this one, with other taking precedence
func (s *DPoPSettings) Merge(other *DPoPSettings) *DPoPSettings {
	if other == nil {
		return s
	}
	
	result := *s
	
	if other.Enabled {
		result.Enabled = other.Enabled
	}
	
	if other.Required {
		result.Required = other.Required
	}
	
	if other.MaxAge != nil {
		result.MaxAge = other.MaxAge
	}
	
	if other.ReplayCacheSize > 0 {
		result.ReplayCacheSize = other.ReplayCacheSize
	}
	
	if other.CacheCleanupInterval != nil {
		result.CacheCleanupInterval = other.CacheCleanupInterval
	}
	
	return &result
}

// Clone creates a deep copy of the DPoPSettings
func (s *DPoPSettings) Clone() *DPoPSettings {
	if s == nil {
		return nil
	}
	
	result := *s
	
	// Deep copy pointer fields
	if s.MaxAge != nil {
		maxAge := *s.MaxAge
		result.MaxAge = &maxAge
	}
	
	if s.CacheCleanupInterval != nil {
		interval := *s.CacheCleanupInterval
		result.CacheCleanupInterval = &interval
	}
	
	return &result
}
