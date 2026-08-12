package clock

// Authoring units for a hold's target.
//
// A hold's target is parts per billion of maxOffset, and it is an integer all
// the way from authoring to evaluation. The ruling behind that is worth keeping
// next to the constructors: the plan *is* replay identity, so a fraction that
// survived into the serialized plan would be multiplied on the replaying
// machine -- and `off + slope*(t-start)` is precisely the multiply-add an arm64
// fuses into one FMA and an amd64 without FMA does not. A last-bit difference
// in a lease expiry is a different history.
//
// So there is no float here to convert. These constructors are the authoring
// vocabulary, and they are exact.

// Percent expresses a hold target as whole percent of maxOffset.
func Percent(n int64) int64 { return n * (ppb / 100) }

// PerMille expresses it in thousandths, for targets like 98.5% that whole
// percent cannot say.
func PerMille(n int64) int64 { return n * (ppb / 1000) }

// PPB is the identity, for a target that wants full resolution.
func PPB(n int64) int64 { return n }
