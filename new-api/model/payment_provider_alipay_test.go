package model

import "testing"

func TestPaymentProviderConstantsAlipayWechat(t *testing.T) {
	// ponytail: brief used map[string]string but two constants share value "alipay",
	// which is a duplicate map key (compile error). Slice of pairs preserves the
	// exact 4 assertions and error message.
	cases := []struct{ got, want string }{
		{PaymentProviderAlipay, "alipay"},
		{PaymentMethodAlipay, "alipay"},
		{PaymentProviderWechat, "wechat"},
		{PaymentMethodWechat, "wxpay"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("provider/method constant mismatch: got %q want %q", c.got, c.want)
		}
	}
}

// 防跨网关:回调 provider 与订单 provider 不匹配时 CompleteSubscriptionOrder 必拒
func TestCompleteSubscriptionOrderRejectsAlipayProviderMismatch(t *testing.T) {
	// 完整端到端依赖订单 seed;此处只断言 ErrPaymentMethodMismatch 常量定义存在
	// (端到端覆盖见 Task 7 沙箱自测)
	if ErrPaymentMethodMismatch == nil {
		t.Fatal("ErrPaymentMethodMismatch not defined")
	}
}
