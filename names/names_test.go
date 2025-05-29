package names

import "testing"

func TestIsSus(t *testing.T) {
	var tests = []struct {
		input string
		want  bool
	}{
		{"", false},
		{"Hello", false},
		{"CLUCKER", false},
		{"Uniswap Reward", true},
		{"Anoncement", true},
		{"📢Big news", true},
		{"📣Important", true},
		{"📡Text here", true},
		{"📡ANNOUNCEMENT", true},
		{"HelloANNOUNCEMENT", true},
		{"Airdrep🔥", true},
		{"Uniswep", true},
		{"ANNOUCENMENT", true},
		// Test cases for Unicode normalization
		{"𝓽𝓱𝓪𝓪𝓬𝓵𝓪𝓼𝓱𝓮𝓻", false}, // Should not match 'support'
		{"ｓｕｐｐｏｒｔ", true},   // Full-width characters, should match 'support'
		{"𝕒𝕚𝕣𝕕𝕣𝕠𝕡", true},     // Mathematical script, should match 'airdrop'
		{"𝖌𝖎𝖛𝖊𝖆𝖜𝖆𝖞", true},   // Fraktur, should match 'giveaway'
		{"ⓗⓔⓛⓟ", true},        // Circled letters, should match 'help'
	}

	for _, test := range tests {
		if got, _ := NameIsSuspicious(test.input); got != test.want {
			t.Errorf("IsSus(%q) = %v", test.input, got)
		}
	}
}
