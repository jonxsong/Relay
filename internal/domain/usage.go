package domain

type Usage struct {
	Provider       string
	Model          string
	PricingVersion *string
	InputTokens    *int64
	OutputTokens   *int64
	CostMicrousd   *int64
}
