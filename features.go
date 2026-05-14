package main

import (
	"math"
	"regexp"
	"strings"
)

const nFeatures = 31

// Feature index constants mirror FEATURE_NAMES order in train_model.py.
const (
	featNLines = iota
	featMeanLineLen
	featMaxLineLen
	featStdLineLen
	featTrailingWsCount
	featMultiSpaceCount
	featLongLineCount
	featBadIndentCount
	featEmptyBlockCount
	featSingleLetterVarCount
	featMagicNumberCount
	featHardcodedCredCount
	featForLoopCount
	featFunctionDefCount
	featFuncNoDocCount
	featHasInitBlock
	featLibSetCount
	featLibSetInInit
	featHasSpinInFor
	featEditBracketCount
	featEditNoSpinCount
	featHasBreakInFor
	featPivotCount
	featUniqAfterPivotCount
	featHasGlobalsGet
	featHTTPCallCount
	featHasParallel
	featPipeCount
	featHasSubqueryPropAssign
	featTrailingWsRatio
	featMultiSpaceRatio
)

// violationCountIndices are the feature indices that directly count rule violations.
// If all are 0, the model uses a raised threshold.
var violationCountIndices = []int{
	featTrailingWsCount,
	featMultiSpaceCount,
	featLongLineCount,
	featBadIndentCount,
	featEmptyBlockCount,
	featSingleLetterVarCount,
	featMagicNumberCount,
	featHardcodedCredCount,
	featFuncNoDocCount,
	featEditNoSpinCount,
}

// compiled regexps — initialised once at package init
var (
	reTrailingWs          = regexp.MustCompile(`(?m)[ \t]+$`)
	reMultiSpace          = regexp.MustCompile(`(?m)(?:[^ \t\n])  +(?:[^ \t\n])`)
	reLinesOver120        = regexp.MustCompile(`(?m).{121,}`)
	reBadIndent3          = regexp.MustCompile(`(?m)^   \S`)
	reEmptyBlock          = regexp.MustCompile(`(?:^|[^(])\{\s*\}(?:[^)]|$)`)
	reSingleLetterVar     = regexp.MustCompile(`\$([ijkxyz])\b`)
	reMagicNumber         = regexp.MustCompile(`\b([2-9]|[1-9]\d+)\b`)
	reHardcodedCred       = regexp.MustCompile(`(?i)(password|apikey|secret|token)\s*=\s*["']`)
	reForLoop             = regexp.MustCompile(`\bfor\b[^\n]*\bin\b`)
	reFunctionDef         = regexp.MustCompile(`(?m)^\s*function\s+\w+`)
	reFuncNoDoc           = regexp.MustCompile(`(?m)^\s*function\s+\w+[^\n]*\n\s*//`)
	reInitBlock           = regexp.MustCompile(`\binit\s*\{`)
	reLibSet              = regexp.MustCompile(`\$lib\.set\(`)
	reLibSetInInit        = regexp.MustCompile(`(?s)init\s*\{[^}]*\$lib\.set\(`)
	reSpinInFor           = regexp.MustCompile(`(?s)for\b[^\n]*\n(?:[^\n]*\n)*?[^\n]*\|\s*spin`)
	reEditBracket         = regexp.MustCompile(`\[\s*[\(\w:*$]`)
	reEditWithSpin        = regexp.MustCompile(`\[\s*[\(\w:*$][^\]]*\]\s*\|\s*spin`)
	reBreakInFor          = regexp.MustCompile(`(?s)for\b[^\n]*\n(?:[^\n]*\n)*?[^\n]*\bbreak\b`)
	rePivot               = regexp.MustCompile(`->`)
	reUniqAfterPivot      = regexp.MustCompile(`->[^\n]*\|\s*uniq`)
	reGlobalsGet          = regexp.MustCompile(`\$lib\.globals\.get\(`)
	reHTTPCall            = regexp.MustCompile(`\$lib\.inet\.http\.`)
	reParallel            = regexp.MustCompile(`\|\s*parallel`)
	rePipeCount           = regexp.MustCompile(`\|`)
	reSubqueryPropAssign  = regexp.MustCompile(`(?s)\{[^}]*\$\w+\s*=\s*:\w+[^}]*\}`)
)

// ExtractFeatures computes the 31-dimensional feature vector for a Storm snippet.
func ExtractFeatures(code string) []float32 {
	lines := strings.Split(code, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	nLines := len(lines)
	lineLens := make([]float64, nLines)
	maxLen := 0
	for i, l := range lines {
		ll := len(l)
		lineLens[i] = float64(ll)
		if ll > maxLen {
			maxLen = ll
		}
	}

	mean := meanF(lineLens)
	std := stdF(lineLens, mean)
	nFuncs := len(reFunctionDef.FindAllString(code, -1))

	trailingWs := len(reTrailingWs.FindAllString(code, -1))
	multiSpace := len(reMultiSpace.FindAllString(code, -1))
	longLines := len(reLinesOver120.FindAllString(code, -1))
	badIndent := len(reBadIndent3.FindAllString(code, -1))
	emptyBlock := len(reEmptyBlock.FindAllString(code, -1))
	singleLetter := len(reSingleLetterVar.FindAllString(code, -1))
	magicNumber := len(reMagicNumber.FindAllString(code, -1))
	hardcodedCred := len(reHardcodedCred.FindAllString(code, -1))
	forLoop := len(reForLoop.FindAllString(code, -1))
	funcWithDoc := len(reFuncNoDoc.FindAllString(code, -1))
	funcNoDoc := nFuncs - funcWithDoc
	if funcNoDoc < 0 {
		funcNoDoc = 0
	}
	libSet := len(reLibSet.FindAllString(code, -1))
	editBracket := len(reEditBracket.FindAllString(code, -1))
	editWithSpin := len(reEditWithSpin.FindAllString(code, -1))
	editNoSpin := editBracket - editWithSpin
	if editNoSpin < 0 {
		editNoSpin = 0
	}
	pivot := len(rePivot.FindAllString(code, -1))
	uniqAfterPivot := len(reUniqAfterPivot.FindAllString(code, -1))
	httpCall := len(reHTTPCall.FindAllString(code, -1))
	pipeCount := len(rePipeCount.FindAllString(code, -1))

	boolToF32 := func(b bool) float32 {
		if b {
			return 1
		}
		return 0
	}

	feats := make([]float32, nFeatures)
	feats[featNLines] = float32(nLines)
	feats[featMeanLineLen] = float32(mean)
	feats[featMaxLineLen] = float32(maxLen)
	feats[featStdLineLen] = float32(std)
	feats[featTrailingWsCount] = float32(trailingWs)
	feats[featMultiSpaceCount] = float32(multiSpace)
	feats[featLongLineCount] = float32(longLines)
	feats[featBadIndentCount] = float32(badIndent)
	feats[featEmptyBlockCount] = float32(emptyBlock)
	feats[featSingleLetterVarCount] = float32(singleLetter)
	feats[featMagicNumberCount] = float32(magicNumber)
	feats[featHardcodedCredCount] = float32(hardcodedCred)
	feats[featForLoopCount] = float32(forLoop)
	feats[featFunctionDefCount] = float32(nFuncs)
	feats[featFuncNoDocCount] = float32(funcNoDoc)
	feats[featHasInitBlock] = boolToF32(reInitBlock.MatchString(code))
	feats[featLibSetCount] = float32(libSet)
	feats[featLibSetInInit] = boolToF32(reLibSetInInit.MatchString(code))
	feats[featHasSpinInFor] = boolToF32(reSpinInFor.MatchString(code))
	feats[featEditBracketCount] = float32(editBracket)
	feats[featEditNoSpinCount] = float32(editNoSpin)
	feats[featHasBreakInFor] = boolToF32(reBreakInFor.MatchString(code))
	feats[featPivotCount] = float32(pivot)
	feats[featUniqAfterPivotCount] = float32(uniqAfterPivot)
	feats[featHasGlobalsGet] = boolToF32(reGlobalsGet.MatchString(code))
	feats[featHTTPCallCount] = float32(httpCall)
	feats[featHasParallel] = boolToF32(reParallel.MatchString(code))
	feats[featPipeCount] = float32(pipeCount)
	feats[featHasSubqueryPropAssign] = boolToF32(reSubqueryPropAssign.MatchString(code))
	feats[featTrailingWsRatio] = float32(trailingWs) / float32(nLines)
	feats[featMultiSpaceRatio] = float32(multiSpace) / float32(nLines)

	return feats
}

// IsDefinitelyClean returns true if all direct violation-count features are zero.
func IsDefinitelyClean(feats []float32) bool {
	for _, idx := range violationCountIndices {
		if feats[idx] != 0 {
			return false
		}
	}
	return true
}

func meanF(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func stdF(xs []float64, mean float64) float64 {
	if len(xs) <= 1 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		d := x - mean
		s += d * d
	}
	return math.Sqrt(s / float64(len(xs)))
}
