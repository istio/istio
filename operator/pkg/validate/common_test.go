package validate

import (
	"regexp"
	"testing"

	"istio.io/istio/operator/pkg/util"
)

func Test_validatePortNumberString(t *testing.T) {
	type args struct {
		path util.Path
		val  any
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "test param not a string",
			args: args{
				path: util.Path{"test"},
				val:  1,
			},
			wantErr: true,
		},
		{
			name: "test port param match any",
			args: args{
				path: util.Path{"test"},
				val:  "*",
			},
			wantErr: false,
		},
		{
			name: "test parse port param error",
			args: args{
				path: util.Path{"test"},
				val:  "invalid",
			},
			wantErr: true,
		},
		{
			name: "test port number out of range",
			args: args{
				path: util.Path{"test"},
				val:  "65536",
			},
			wantErr: true,
		},
		{
			name: "test validate port number pass",
			args: args{
				path: util.Path{"test"},
				val:  "80",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validatePortNumberString(tt.args.path, tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("validatePortNumberString() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_validateIPRangesOrStar(t *testing.T) {
	type args struct {
		path util.Path
		val  any
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "test param not a string",
			args: args{
				path: util.Path{"test"},
				val:  1,
			},
			wantErr: true,
		},
		{
			name: "test ip range param match any",
			args: args{
				path: util.Path{"test"},
				val:  "*",
			},
			wantErr: false,
		},
		{
			name: "test validate ip range error",
			args: args{
				path: util.Path{"test"},
				val:  "1.1.0.256/16",
			},
			wantErr: true,
		},
		{
			name: "test validate ip range pass",
			args: args{
				path: util.Path{"test"},
				val:  "1.1.0.254/16",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateIPRangesOrStar(tt.args.path, tt.args.val); (err != nil) != tt.wantErr {
				t.Errorf("validateIPRangesOrStar() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_validateWithRegex(t *testing.T) {
	type args struct {
		path util.Path
		val  any
		r    *regexp.Regexp
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "test match regex string",
			args: args{
				path: util.Path{"test"},
				val:  "test",
				r:    regexp.MustCompile(`^test$`),
			},
			wantErr: false,
		},
		{
			name: "test not match regex string",
			args: args{
				path: util.Path{"test"},
				val:  "test",
				r:    regexp.MustCompile(`^test1$`),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateWithRegex(tt.args.path, tt.args.val, tt.args.r); (err != nil) != tt.wantErr {
				t.Errorf("validateWithRegex() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func Test_validateIntRange(t *testing.T) {
	type args struct {
		path util.Path
		val  any
		min  int64
		max  int64
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "test param not a number",
			args: args{
				path: util.Path{"test"},
				val:  "1",
				min:  1,
				max:  10,
			},
			wantErr: true,
		},
		{
			name: "test param is uint but out of range",
			args: args{
				path: util.Path{"test"},
				val:  uint(11),
				min:  1,
				max:  10,
			},
			wantErr: true,
		},
		{
			name: "test param is uint and in range",
			args: args{
				path: util.Path{"test"},
				val:  uint(5),
				min:  1,
				max:  10,
			},
			wantErr: false,
		},
		{
			name: "test param is int and in range",
			args: args{
				path: util.Path{"test"},
				val:  5,
				min:  1,
				max:  10,
			},
			wantErr: false,
		},
		{
			name: "test param is int but out of range",
			args: args{
				path: util.Path{"test"},
				val:  11,
				min:  1,
				max:  10,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateIntRange(tt.args.path, tt.args.val, tt.args.min, tt.args.max); (err != nil) != tt.wantErr {
				t.Errorf("validateIntRange() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
