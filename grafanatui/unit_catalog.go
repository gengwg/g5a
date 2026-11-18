package grafanatui

import "math"

// catalogUnitFormatters maps Grafana unit IDs that are a symbol plus a scaling
// family to formatters. The entries are ported from Grafana's
// packages/grafana-data/src/valueFormats/categories.ts (commit 4f2bebd4a0).
// IDs with a dedicated formatter in unitFormatters are left out.
var catalogUnitFormatters = map[string]func(float64, *int) string{ //nolint:gochecknoglobals // read-only registry
	"sishort":       siUnit("", 0),
	"humidity":      fixedUnit("%H"),
	"dB":            fixedUnit("dB"),
	"candela":       siUnit("cd", 0),
	"pixel":         fixedUnit("px"),
	"accMS2":        fixedUnit("m/sec²"),
	"accFS2":        fixedUnit("f/sec²"),
	"accG":          fixedUnit("g"),
	"degree":        fixedUnit("°"),
	"radian":        fixedUnit("rad"),
	"grad":          fixedUnit("grad"),
	"arcmin":        fixedUnit("arcmin"),
	"arcsec":        fixedUnit("arcsec"),
	"areaM2":        fixedUnit("m²"),
	"areaF2":        fixedUnit("ft²"),
	"areaMI2":       fixedUnit("mi²"),
	"acres":         fixedUnit("ac"),
	"hectares":      fixedUnit("ha"),
	"flops":         siUnit("FLOPS", 0),
	"mflops":        siUnit("FLOPS", 2),
	"gflops":        siUnit("FLOPS", 3),
	"tflops":        siUnit("FLOPS", 4),
	"pflops":        siUnit("FLOPS", 5),
	"eflops":        siUnit("FLOPS", 6),
	"zflops":        siUnit("FLOPS", 7),
	"yflops":        siUnit("FLOPS", 8),
	"ppm":           fixedUnit("ppm"),
	"conppb":        fixedUnit("ppb"),
	"conngm3":       fixedUnit("ng/m³"),
	"conngNm3":      fixedUnit("ng/Nm³"),
	"conμgm3":       fixedUnit("μg/m³"),
	"conμgNm3":      fixedUnit("μg/Nm³"),
	"conmgm3":       fixedUnit("mg/m³"),
	"conmgNm3":      fixedUnit("mg/Nm³"),
	"congm3":        fixedUnit("g/m³"),
	"congNm3":       fixedUnit("g/Nm³"),
	"conmgdL":       fixedUnit("mg/dL"),
	"conmmolL":      fixedUnit("mmol/L"),
	"currencyUSD":   currencyUnit("$", false),
	"currencyGBP":   currencyUnit("£", false),
	"currencyEUR":   currencyUnit("€", false),
	"currencyJPY":   currencyUnit("¥", false),
	"currencyRUB":   currencyUnit("₽", false),
	"currencyUAH":   currencyUnit("₴", false),
	"currencyBRL":   currencyUnit("R$", false),
	"currencyDKK":   currencyUnit("kr", true),
	"currencyISK":   currencyUnit("kr", true),
	"currencyNOK":   currencyUnit("kr", true),
	"currencySEK":   currencyUnit("kr", true),
	"currencyCZK":   currencyUnit("czk", false),
	"currencyCHF":   currencyUnit("CHF", false),
	"currencyPLN":   currencyUnit("PLN", false),
	"currencyBTC":   currencyUnit("฿", false),
	"currencymBTC":  currencyUnit("mBTC", false),
	"currencyμBTC":  currencyUnit("μBTC", false),
	"currencyZAR":   currencyUnit("R", false),
	"currencyINR":   currencyUnit("₹", false),
	"currencyKRW":   currencyUnit("₩", false),
	"currencyIDR":   currencyUnit("Rp", false),
	"currencyPHP":   currencyUnit("PHP", false),
	"currencyVND":   currencyUnit("đ", true),
	"currencyTRY":   currencyUnit("₺", true),
	"currencyMYR":   currencyUnit("RM", false),
	"currencyXPF":   currencyUnit("XPF", false),
	"currencyBGN":   currencyUnit("BGN", false),
	"currencyPYG":   currencyUnit("₲", false),
	"currencyUYU":   currencyUnit("UYU", false),
	"currencyILS":   currencyUnit("₪", false),
	"decbits":       siUnit("b", 0),
	"kbytes":        binaryUnit("B", 1),
	"deckbytes":     siUnit("B", 1),
	"mbytes":        binaryUnit("B", 2),
	"decmbytes":     siUnit("B", 2),
	"gbytes":        binaryUnit("B", 3),
	"decgbytes":     siUnit("B", 3),
	"tbytes":        binaryUnit("B", 4),
	"dectbytes":     siUnit("B", 4),
	"pbytes":        binaryUnit("B", 5),
	"decpbytes":     siUnit("B", 5),
	"binbps":        binaryUnit("b/s", 0),
	"KiBs":          binaryUnit("B/s", 1),
	"Kibits":        binaryUnit("b/s", 1),
	"KBs":           siUnit("B/s", 1),
	"Kbits":         siUnit("b/s", 1),
	"MiBs":          binaryUnit("B/s", 2),
	"Mibits":        binaryUnit("b/s", 2),
	"MBs":           siUnit("B/s", 2),
	"Mbits":         siUnit("b/s", 2),
	"GiBs":          binaryUnit("B/s", 3),
	"Gibits":        binaryUnit("b/s", 3),
	"GBs":           siUnit("B/s", 3),
	"Gbits":         siUnit("b/s", 3),
	"TiBs":          binaryUnit("B/s", 4),
	"Tibits":        binaryUnit("b/s", 4),
	"TBs":           siUnit("B/s", 4),
	"Tbits":         siUnit("b/s", 4),
	"PiBs":          binaryUnit("B/s", 5),
	"Pibits":        binaryUnit("b/s", 5),
	"PBs":           siUnit("B/s", 5),
	"Pbits":         siUnit("b/s", 5),
	"megwatt":       siUnit("W", 2),
	"gwatt":         siUnit("W", 3),
	"mwatt":         siUnit("W", -1),
	"Wm2":           siUnit("W/m²", 0),
	"kvoltamp":      siUnit("VA", 1),
	"voltampreact":  siUnit("VAr", 0),
	"kvoltampreact": siUnit("VAr", 1),
	"watth":         siUnit("Wh", 0),
	"watthperkg":    siUnit("Wh/kg", 0),
	"kwatth":        siUnit("Wh", 1),
	"kwattm":        siUnit("W-Min", 1),
	"mwatth":        siUnit("Wh", 2),
	"amph":          siUnit("Ah", 0),
	"kamph":         siUnit("Ah", 1),
	"mamph":         siUnit("Ah", -1),
	"joule":         siUnit("J", 0),
	"ev":            siUnit("eV", 0),
	"amp":           siUnit("A", 0),
	"kamp":          siUnit("A", 1),
	"mamp":          siUnit("A", -1),
	"volt":          siUnit("V", 0),
	"kvolt":         siUnit("V", 1),
	"mvolt":         siUnit("V", -1),
	"dBm":           fixedUnit("dBm"),
	"mohm":          siUnit("Ω", -1),
	"ohm":           siUnit("Ω", 0),
	"kohm":          siUnit("Ω", 1),
	"Mohm":          siUnit("Ω", 2),
	"farad":         siUnit("F", 0),
	"µfarad":        siUnit("F", -2),
	"nfarad":        siUnit("F", -3),
	"pfarad":        siUnit("F", -4),
	"ffarad":        siUnit("F", -5),
	"henry":         siUnit("H", 0),
	"mhenry":        siUnit("H", -1),
	"µhenry":        siUnit("H", -2),
	"lumens":        siUnit("Lm", 0),
	"flowgpm":       fixedUnit("gpm"),
	"flowcms":       fixedUnit("cms"),
	"flowcfs":       fixedUnit("cfs"),
	"flowcfm":       fixedUnit("cfm"),
	"litreh":        fixedUnit("L/h"),
	"flowlpm":       fixedUnit("L/min"),
	"flowmlpm":      fixedUnit("mL/min"),
	"lux":           fixedUnit("lux"),
	"forceNm":       siUnit("Nm", 0),
	"forcekNm":      siUnit("Nm", 1),
	"forceN":        siUnit("N", 0),
	"forcekN":       siUnit("N", 1),
	"Hs":            siUnit("H/s", 0),
	"KHs":           siUnit("H/s", 1),
	"MHs":           siUnit("H/s", 2),
	"GHs":           siUnit("H/s", 3),
	"THs":           siUnit("H/s", 4),
	"PHs":           siUnit("H/s", 5),
	"EHs":           siUnit("H/s", 6),
	"massmg":        siUnit("g", -1),
	"massg":         siUnit("g", 0),
	"masslb":        fixedUnit("lb"),
	"masskg":        siUnit("g", 1),
	"masst":         fixedUnit("t"),
	"lengthmm":      siUnit("m", -1),
	"lengthcm":      fixedUnit("cm"),
	"lengthin":      fixedUnit("in"),
	"lengthft":      fixedUnit("ft"),
	"lengthm":       siUnit("m", 0),
	"lengthkm":      siUnit("m", 1),
	"lengthmi":      fixedUnit("mi"),
	"pressurembar":  siUnit("bar", -1),
	"pressurebar":   siUnit("bar", 0),
	"pressurekbar":  siUnit("bar", 1),
	"pressurepa":    siUnit("Pa", 0),
	"pressurehpa":   fixedUnit("hPa"),
	"pressurekpa":   fixedUnit("kPa"),
	"pressurehg":    fixedUnit("\"Hg"),
	"radbq":         siUnit("Bq", 0),
	"radci":         siUnit("Ci", 0),
	"radgy":         siUnit("Gy", 0),
	"radrad":        siUnit("rad", 0),
	"radsv":         siUnit("Sv", 0),
	"radmsv":        siUnit("Sv", -1),
	"radusv":        siUnit("Sv", -2),
	"radrem":        siUnit("rem", 0),
	"radexpckg":     siUnit("C/kg", 0),
	"radr":          siUnit("R", 0),
	"radsvh":        siUnit("Sv/h", 0),
	"radmsvh":       siUnit("Sv/h", -1),
	"radusvh":       siUnit("Sv/h", -2),
	"rotrpm":        fixedUnit("rpm"),
	"rothz":         siUnit("Hz", 0),
	"rotkhz":        siUnit("Hz", 1),
	"rotmhz":        siUnit("Hz", 2),
	"rotghz":        siUnit("Hz", 3),
	"rotrads":       fixedUnit("rad/s"),
	"rotdegs":       fixedUnit("°/s"),
	"cps":           countUnit("c/s"),
	"rps":           countUnit("rd/s"),
	"wps":           countUnit("wr/s"),
	"eps":           countUnit("evt/s"),
	"mps":           countUnit("msg/s"),
	"recps":         countUnit("rec/s"),
	"rowsps":        countUnit("rows/s"),
	"cpm":           countUnit("c/m"),
	"opm":           countUnit("ops/m"),
	"reqpm":         countUnit("req/m"),
	"rpm":           countUnit("rd/m"),
	"wpm":           countUnit("wr/m"),
	"epm":           countUnit("evts/m"),
	"mpm":           countUnit("msgs/m"),
	"recpm":         countUnit("rec/m"),
	"rowspm":        countUnit("rows/m"),
	"mlitre":        siUnit("L", -1),
	"litre":         siUnit("L", 0),
	"m3":            fixedUnit("m³"),
	"Nm3":           fixedUnit("Nm³"),
	"dm3":           fixedUnit("dm³"),
	"gallons":       fixedUnit("gal"),
}

//nolint:gochecknoglobals // read-only prefix tables
var (
	siPrefixes     = []string{"f", "p", "n", "µ", "m", "", "k", "M", "G", "T", "P", "E", "Z", "Y"}
	binaryPrefixes = []string{"", "Ki", "Mi", "Gi", "Ti", "Pi", "Ei", "Zi", "Yi"}
)

const siBaseIndex = 5

func fixedUnit(symbol string) func(float64, *int) string {
	return func(value float64, decimals *int) string {
		return formatRawNumber(value, decimals) + " " + symbol
	}
}

func countUnit(symbol string) func(float64, *int) string {
	return func(value float64, decimals *int) string {
		return formatCountUnit(value, symbol, decimals)
	}
}

// currencyUnit follows Grafana: the sign goes before the symbol, and a suffix
// symbol is appended directly after the K/M/B/T scale.
func currencyUnit(symbol string, asSuffix bool) func(float64, *int) string {
	return func(value float64, decimals *int) string {
		sign := ""
		if value < 0 {
			sign = "-"
			value = -value
		}

		number := formatCountScaled(value, decimals)
		if asSuffix {
			return sign + number + symbol
		}

		return sign + symbol + number
	}
}

// siUnit formats a unit whose own SI prefix is offset steps from the base,
// so "kwatth" is siUnit("Wh", 1).
func siUnit(unit string, offset int) func(float64, *int) string {
	return func(value float64, decimals *int) string {
		return formatScaledUnit(value, siKilo, siPrefixes, siBaseIndex+offset, unit, decimals)
	}
}

func binaryUnit(unit string, offset int) func(float64, *int) string {
	return func(value float64, decimals *int) string {
		return formatScaledUnit(value, binaryKibi, binaryPrefixes, offset, unit, decimals)
	}
}

// formatScaledUnit moves the value one prefix per factor until it is at
// least 1 and below factor, within the available prefixes.
func formatScaledUnit(value, factor float64, prefixes []string, index int, unit string, decimals *int) string {
	for math.Abs(value) >= factor && index < len(prefixes)-1 {
		value /= factor
		index++
	}

	for value != 0 && math.Abs(value) < 1 && index > 0 {
		value *= factor
		index--
	}

	return formatUnitNumber(value, " "+prefixes[index]+unit, decimals)
}
