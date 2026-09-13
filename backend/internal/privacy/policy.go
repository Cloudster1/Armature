// Package privacy gives personal data a lifetime: rows that have served their
// purpose are pruned on a schedule, a person can take what is theirs and
// leave, and an organization can let a member go.
package privacy

import "github.com/armature/armature/backend/internal/config"

// Policy is the retention policy, as configuration names it.
type Policy = config.Retention

// DefaultPolicy is what a fresh installation keeps.
func DefaultPolicy() Policy { return config.DefaultRetention() }
