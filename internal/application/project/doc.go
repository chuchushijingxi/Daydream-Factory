// Package project 实现导入、提取/纠错、资产管理及内容生成用例。
// 用例通过端口协调快照/CAS、模型、费用与恢复，保持模型理解和程序保存的边界。
// Prompt、模型可写 DTO、schema 与工具采用协议集中在本包，领域对象不直接充当提取 DTO。
package project
