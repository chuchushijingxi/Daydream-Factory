// Package billing 定义用量、精确计价和预留计算，不管理供应商请求或数据库事务。
// 未知用量不能伪造零费用，外发许可及结算顺序由应用用例控制。
package billing
