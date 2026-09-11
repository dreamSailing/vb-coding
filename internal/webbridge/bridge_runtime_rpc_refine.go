package webbridge

import (
	"context"
	"strings"
)

// AI 优化表达 RPC：把用户输入草稿一次性改写为更清晰专业的描述。

func (s *BridgeService) refineInputRPC(ctx context.Context, draft string) (string, error) {
	gateway, err := requireRuntimeGateway(s)
	if err != nil {
		return "", err
	}
	text, err := coreOnlyResult(
		gateway,
		func(g bridgeRuntimeGateway) (string, error) { return g.CoreRefineInputRPC(ctx, draft) },
	)
	return strings.TrimSpace(text), err
}
