package plugin

import "strings"

// Compare the already validated v5 SemVer spelling without integer overflow.
func v5VersionBefore(a, b string) bool {
	left, right := strings.SplitN(a, "-", 2), strings.SplitN(b, "-", 2)
	numeric := func(a, b string) int {
		a = strings.TrimLeft(a, "0")
		b = strings.TrimLeft(b, "0")
		if len(a) < len(b) {
			return -1
		}
		if len(a) > len(b) {
			return 1
		}
		return strings.Compare(a, b)
	}
	ac, bc := strings.Split(left[0], "."), strings.Split(right[0], ".")
	for n := 0; n < 3; n++ {
		if d := numeric(ac[n], bc[n]); d != 0 {
			return d < 0
		}
	}
	if len(left) == 1 {
		return false
	}
	if len(right) == 1 {
		return true
	}
	ap, bp := strings.Split(left[1], "."), strings.Split(right[1], ".")
	isNumber := func(v string) bool {
		if v == "" {
			return false
		}
		for _, c := range v {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	for n := 0; n < min(len(ap), len(bp)); n++ {
		an, bn := isNumber(ap[n]), isNumber(bp[n])
		d := 0
		if an && bn {
			d = numeric(ap[n], bp[n])
		} else if an != bn {
			if an {
				return true
			}
			return false
		} else {
			d = strings.Compare(ap[n], bp[n])
		}
		if d != 0 {
			return d < 0
		}
	}
	return len(ap) < len(bp)
}
