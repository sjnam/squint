# squint

M. Douglas McIlroy의 논문 [Squinting at Power Series](squint.pdf)에 나오는 멱급수(power series)
스트림 연산들을 Go의 고루틴과 채널로 구현한 패키지입니다.

멱급수는 유리수 계수(`math/big.Rat`)의 무한 스트림으로 표현되며, 차수는 위치로
암시됩니다. 각 연산은 고루틴으로 실행되고 채널로 연결됩니다.

## Demand channel

논문의 핵심 장치인 **demand channel**을 그대로 따릅니다. 급수 하나는 요청(`req`)과
데이터(`dat`) 두 채널의 쌍이며, 소비자가 `req`로 신호를 보내야만 다음 계수가
계산됩니다. 이로써 수요를 앞질러 계산하는 runaway가 없는 완전한 lazy evaluation이
강제됩니다.

```go
type PS struct {
    ctx context.Context
    req chan struct{}
    dat chan *big.Rat
}
```

## 수명 관리

급수 하나는 실행 중인 프로세스 네트워크의 핸들입니다. 생성기(`Ones`, `Series`, `X`)는
`context.Context`를 받고, 파생 연산은 입력 급수의 컨텍스트를 상속합니다. 컨텍스트를
cancel하면 네트워크의 모든 고루틴이 종료됩니다. cancel된 뒤 `Get`은 `nil`을,
`Take`는 그때까지 얻은 항만 반환합니다. 서로 다른 컨텍스트에서 만든 급수를 섞으면
안 됩니다.

```go
ctx, cancel := context.WithCancel(context.Background())
tan := squint.Rev(squint.Integ(zero, squint.Msubst(squint.Ones(ctx), negOne, 2)))
tan.Take(20)
cancel() // 모든 고루틴 종료
```

## 연산

| 함수 | 의미 | 논문의 식 |
| --- | --- | --- |
| `Add(F, G)` | F + G | |
| `Cmul(c, F)` | c·F | |
| `Xmul(F)` | x·F (한 항 지연) | |
| `Deriv(F)` | F′ | |
| `Integ(c, F)` | ∫F + c | |
| `Mul(F, G)` | F·G | (2) |
| `Subst(F, G)` | F(G), G₀ = 0 | (3) |
| `Msubst(F, c, n)` | F(c·xⁿ) | |
| `Exp(F)` | e^F, F₀ = 0 — 적분이 자기 자신을 먹는 Picard 반복 | X = ∫X·F′ + 1 |
| `Recip(F)` | 1/F, F₀ ≠ 0 | |
| `Rev(F)` | 함수적 역원: F(R(x)) = x | (8) |
| `Split(F, n)` | F를 n개의 동일한 스트림으로 분기 (fanout + queue) | |

`Split`은 논문의 do_split 프로세스 체인 대신, 큐를 가진 단일 서버 고루틴으로
구현했습니다. 다음 항을 원천에서 가져오는 동작은 비동기로 수행하여, 자기 자신의
split을 통해 재귀적으로 정의되는 급수(`Exp`, `Recip`, `Rev`)에서도 교착이 생기지
않습니다.

## 실행

```sh
go test ./...
go run ./cmd/squint
```

논문의 마지막 예제 — arctan을 적분으로 만들고 reversion으로 tan을 얻기:

```go
arctan := squint.Integ(zero, squint.Msubst(squint.Ones(ctx), negOne, 2))
tan := squint.Rev(arctan)
```

```text
tan(x) = rev(arctan):  0 1 0 1/3 0 2/15 0 17/315 0 62/2835 0 1382/155925
```
