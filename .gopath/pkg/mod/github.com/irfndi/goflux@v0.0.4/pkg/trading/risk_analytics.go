package trading

import (
	"math"
	"time"

	"github.com/irfndi/goflux/pkg/decimal"
)

type VaRMethod int

const (
	HistoricalVaR VaRMethod = iota
	ParametricVaR
	MonteCarloVaR
)

type VaRResult struct {
	VaR        decimal.Decimal
	CVaR       decimal.Decimal
	Confidence decimal.Decimal
	Period     int
}

func NewVaRResult(varVal, cvar, confidence decimal.Decimal, period int) *VaRResult {
	return &VaRResult{
		VaR:        varVal,
		CVaR:       cvar,
		Confidence: confidence,
		Period:     period,
	}
}

type VaRCalculator struct {
	method     VaRMethod
	confidence decimal.Decimal
	period     int
}

func NewVaRCalculator(method VaRMethod, confidence float64, period int) *VaRCalculator {
	return &VaRCalculator{
		method:     method,
		confidence: decimal.New(confidence),
		period:     period,
	}
}

func (vc *VaRCalculator) Calculate(returns []decimal.Decimal) *VaRResult {
	if len(returns) == 0 {
		return NewVaRResult(decimal.ZERO, decimal.ZERO, vc.confidence, vc.period)
	}

	switch vc.method {
	case HistoricalVaR:
		return vc.historicalVaR(returns)
	case ParametricVaR:
		return vc.parametricVaR(returns)
	case MonteCarloVaR:
		return vc.monteCarloVaR(returns, 10000)
	default:
		return vc.historicalVaR(returns)
	}
}

func (vc *VaRCalculator) historicalVaR(returns []decimal.Decimal) *VaRResult {
	sortedReturns := make([]decimal.Decimal, len(returns))
	copy(sortedReturns, returns)

	for i := range sortedReturns {
		for j := i + 1; j < len(sortedReturns); j++ {
			if sortedReturns[i].GT(sortedReturns[j]) {
				sortedReturns[i], sortedReturns[j] = sortedReturns[j], sortedReturns[i]
			}
		}
	}

	confidenceIndex := int((1 - vc.confidence.Float()) * float64(len(sortedReturns)))
	if confidenceIndex >= len(sortedReturns) {
		confidenceIndex = len(sortedReturns) - 1
	}
	if confidenceIndex < 0 {
		confidenceIndex = 0
	}

	varLoss := sortedReturns[confidenceIndex]

	var cvarSum decimal.Decimal
	count := 0
	for i := 0; i <= confidenceIndex; i++ {
		cvarSum = cvarSum.Add(sortedReturns[i])
		count++
	}

	var cvar decimal.Decimal
	if count > 0 {
		cvar = cvarSum.Div(decimal.New(float64(count)))
	}

	return NewVaRResult(varLoss.Neg(), cvar.Neg(), vc.confidence, vc.period)
}

func (vc *VaRCalculator) parametricVaR(returns []decimal.Decimal) *VaRResult {
	if len(returns) == 0 {
		return NewVaRResult(decimal.ZERO, decimal.ZERO, vc.confidence, vc.period)
	}

	mean := vc.calculateMean(returns)
	variance := vc.calculateVariance(returns, mean)
	stdDev := variance.Sqrt()

	zScore := vc.getZScore(vc.confidence.Float())
	varLoss := mean.Sub(stdDev.Mul(zScore))

	zScoreFloat := zScore.Float()
	alpha := vc.confidence.Float()
	phi := getNormalPDF(zScoreFloat)
	cvarVal := mean.Sub(stdDev.Mul(decimal.New(zScoreFloat + phi/(1-alpha))))

	return NewVaRResult(varLoss.Neg(), cvarVal.Neg(), vc.confidence, vc.period)
}

func (vc *VaRCalculator) monteCarloVaR(returns []decimal.Decimal, simulations int) *VaRResult {
	if len(returns) == 0 {
		return NewVaRResult(decimal.ZERO, decimal.ZERO, vc.confidence, vc.period)
	}

	if simulations <= 0 {
		simulations = 10000
	}

	mean := vc.calculateMean(returns)
	variance := vc.calculateVariance(returns, mean)
	stdDev := variance.Sqrt()

	simulatedReturns := make([]decimal.Decimal, simulations)
	for i := 0; i < simulations; i++ {
		randomNormal := boxMullerTransform()
		simulatedReturns[i] = mean.Add(stdDev.Mul(decimal.New(randomNormal)))
	}

	sortedReturns := make([]decimal.Decimal, simulations)
	copy(sortedReturns, simulatedReturns)
	for i := range sortedReturns {
		for j := i + 1; j < len(sortedReturns); j++ {
			if sortedReturns[i].GT(sortedReturns[j]) {
				sortedReturns[i], sortedReturns[j] = sortedReturns[j], sortedReturns[i]
			}
		}
	}

	confidenceIndex := int((1 - vc.confidence.Float()) * float64(simulations))
	if confidenceIndex >= simulations {
		confidenceIndex = simulations - 1
	}
	if confidenceIndex < 0 {
		confidenceIndex = 0
	}

	varLoss := sortedReturns[confidenceIndex]

	var cvarSum decimal.Decimal
	count := 0
	for i := 0; i <= confidenceIndex; i++ {
		cvarSum = cvarSum.Add(sortedReturns[i])
		count++
	}

	var cvar decimal.Decimal
	if count > 0 {
		cvar = cvarSum.Div(decimal.New(float64(count)))
	}

	return NewVaRResult(varLoss.Neg(), cvar.Neg(), vc.confidence, vc.period)
}

func (vc *VaRCalculator) calculateMean(returns []decimal.Decimal) decimal.Decimal {
	var sum decimal.Decimal
	for _, r := range returns {
		sum = sum.Add(r)
	}
	return sum.Div(decimal.New(float64(len(returns))))
}

func (vc *VaRCalculator) calculateVariance(returns []decimal.Decimal, mean decimal.Decimal) decimal.Decimal {
	var sumSq decimal.Decimal
	for _, r := range returns {
		diff := r.Sub(mean)
		sumSq = sumSq.Add(diff.Mul(diff))
	}
	return sumSq.Div(decimal.New(float64(len(returns) - 1)))
}

func (vc *VaRCalculator) getZScore(confidence float64) decimal.Decimal {
	return getZScoreForConfidence(confidence)
}

func boxMullerTransform() float64 {
	u1 := randFloat64()
	u2 := randFloat64()
	return math.Sqrt(-2.0*math.Log(u1)) * math.Cos(2.0*math.Pi*u2)
}

func randFloat64() float64 {
	return float64(generateRandomInt()) / float64(1<<31)
}

func generateRandomInt() int {
	return int(time.Now().UnixNano() % int64(1<<31))
}

func getNormalPDF(z float64) float64 {
	return math.Exp(-0.5*z*z) / math.Sqrt(2*math.Pi)
}

func getZScoreForConfidence(confidence float64) decimal.Decimal {
	switch {
	case confidence >= 0.99:
		return decimal.New(2.326)
	case confidence >= 0.975:
		return decimal.New(1.96)
	case confidence >= 0.95:
		return decimal.New(1.645)
	case confidence >= 0.90:
		return decimal.New(1.282)
	case confidence >= 0.80:
		return decimal.New(0.842)
	case confidence >= 0.75:
		return decimal.New(0.674)
	default:
		return decimal.New(1.0)
	}
}

func CalculateReturns(prices []decimal.Decimal) []decimal.Decimal {
	if len(prices) < 2 {
		return []decimal.Decimal{}
	}

	returns := make([]decimal.Decimal, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		returns[i-1] = prices[i].Sub(prices[i-1]).Div(prices[i-1])
	}
	return returns
}

func CalculateLogReturns(prices []decimal.Decimal) []decimal.Decimal {
	if len(prices) < 2 {
		return []decimal.Decimal{}
	}

	logReturns := make([]decimal.Decimal, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		ratio := prices[i].Div(prices[i-1])
		logReturns[i-1] = decimal.New(math.Log(ratio.Float()))
	}
	return logReturns
}

func CalculatePortfolioVaR(returns []decimal.Decimal, weights []decimal.Decimal, confidence float64) *VaRResult {
	if len(returns) == 0 || len(weights) == 0 {
		return NewVaRResult(decimal.ZERO, decimal.ZERO, decimal.New(confidence), 1)
	}

	varCalc := NewVaRCalculator(HistoricalVaR, confidence, 1)
	return varCalc.Calculate(returns)
}

func CalculateRollingVaR(prices []decimal.Decimal, window int, confidence float64, method VaRMethod) []*VaRResult {
	if len(prices) < window+1 {
		return []*VaRResult{}
	}

	varCalc := NewVaRCalculator(method, confidence, window)
	results := make([]*VaRResult, 0, len(prices)-window)

	for i := window; i < len(prices); i++ {
		windowPrices := prices[i-window : i+1]
		returns := CalculateReturns(windowPrices)
		if len(returns) > 0 {
			result := varCalc.Calculate(returns)
			results = append(results, result)
		}
	}

	return results
}

func CalculateMaximumDrawdown(equityCurve []decimal.Decimal) decimal.Decimal {
	if len(equityCurve) == 0 {
		return decimal.ZERO
	}

	maxDrawdown := decimal.ZERO
	peak := equityCurve[0]

	for _, value := range equityCurve {
		if value.GT(peak) {
			peak = value
		}

		drawdown := peak.Sub(value)
		if drawdown.GT(maxDrawdown) {
			maxDrawdown = drawdown
		}
	}

	return maxDrawdown
}

func CalculateCalmarRatio(annualizedReturn, maxDrawdown decimal.Decimal) decimal.Decimal {
	if maxDrawdown.IsZero() {
		return decimal.ZERO
	}
	return annualizedReturn.Div(maxDrawdown)
}

func CalculateSortinoRatio(returns []decimal.Decimal, targetReturn decimal.Decimal) decimal.Decimal {
	if len(returns) == 0 {
		return decimal.ZERO
	}

	mean := decimal.ZERO
	for _, r := range returns {
		mean = mean.Add(r)
	}
	mean = mean.Div(decimal.New(float64(len(returns))))

	downsideVariance := decimal.ZERO
	for _, r := range returns {
		if r.LT(targetReturn) {
			diff := targetReturn.Sub(r)
			downsideVariance = downsideVariance.Add(diff.Mul(diff))
		}
	}

	if len(returns) > 0 {
		downsideVariance = downsideVariance.Div(decimal.New(float64(len(returns))))
	}

	downsideDeviation := downsideVariance.Sqrt()
	if downsideDeviation.IsZero() {
		return decimal.New(100)
	}

	return mean.Sub(targetReturn).Div(downsideDeviation)
}

func CalculateBeta(stockReturns, marketReturns []decimal.Decimal) decimal.Decimal {
	if len(stockReturns) == 0 || len(marketReturns) == 0 {
		return decimal.ONE
	}

	stockMean := decimal.ZERO
	marketMean := decimal.ZERO

	for _, r := range stockReturns {
		stockMean = stockMean.Add(r)
	}
	stockMean = stockMean.Div(decimal.New(float64(len(stockReturns))))

	for _, r := range marketReturns {
		marketMean = marketMean.Add(r)
	}
	marketMean = marketMean.Div(decimal.New(float64(len(marketReturns))))

	covariance := decimal.ZERO
	marketVariance := decimal.ZERO

	for i := 0; i < len(stockReturns) && i < len(marketReturns); i++ {
		stockDiff := stockReturns[i].Sub(stockMean)
		marketDiff := marketReturns[i].Sub(marketMean)
		covariance = covariance.Add(stockDiff.Mul(marketDiff))
		marketVariance = marketVariance.Add(marketDiff.Mul(marketDiff))
	}

	if marketVariance.IsZero() {
		return decimal.ONE
	}

	return covariance.Div(marketVariance)
}

func CalculateCorrelation(series1, series2 []decimal.Decimal) decimal.Decimal {
	if len(series1) == 0 || len(series2) == 0 {
		return decimal.ZERO
	}

	mean1 := decimal.ZERO
	mean2 := decimal.ZERO

	for _, v := range series1 {
		mean1 = mean1.Add(v)
	}
	mean1 = mean1.Div(decimal.New(float64(len(series1))))

	for _, v := range series2 {
		mean2 = mean2.Add(v)
	}
	mean2 = mean2.Div(decimal.New(float64(len(series2))))

	covariance := decimal.ZERO
	stdDev1Sq := decimal.ZERO
	stdDev2Sq := decimal.ZERO

	for i := 0; i < len(series1) && i < len(series2); i++ {
		diff1 := series1[i].Sub(mean1)
		diff2 := series2[i].Sub(mean2)
		covariance = covariance.Add(diff1.Mul(diff2))
		stdDev1Sq = stdDev1Sq.Add(diff1.Mul(diff1))
		stdDev2Sq = stdDev2Sq.Add(diff2.Mul(diff2))
	}

	denominator := stdDev1Sq.Sqrt().Mul(stdDev2Sq.Sqrt())
	if denominator.IsZero() {
		return decimal.ZERO
	}

	return covariance.Div(denominator)
}
