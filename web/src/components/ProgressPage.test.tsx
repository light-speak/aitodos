import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ProgressPage } from './ProgressPage'

describe('ProgressPage', () => {
	it('区分严格进度、估算进度和测试证据', () => {
		render(<ProgressPage loading={false} error={null} progress={{
			total_tasks: 8, accepted_tasks: 3, strict_percent: 37.5,
			estimated_tasks: 6, estimate_coverage: 75, total_points: 21,
			remaining_points: 8, forecast_percent: 61.9,
			required_tests: 10, verified_passed_tests: 7, agent_reported_passed_tests: 2,
		}} usage={{
			total_runs: 4, runs_with_usage: 2,
			input_tokens: 87953, cached_input_tokens: 67328, uncached_input_tokens: 20625,
			output_tokens: 1201, reasoning_output_tokens: 359,
			by_purpose: [{ purpose: 'IMPLEMENTATION', total_runs: 2, runs_with_usage: 1, input_tokens: 71420, cached_input_tokens: 56320, uncached_input_tokens: 15100, output_tokens: 916, reasoning_output_tokens: 276 }],
		}} onReload={() => undefined} />)

		expect(screen.getByText('37.5%')).toBeInTheDocument()
		expect(screen.getByText('61.9%')).toBeInTheDocument()
		expect(screen.getByText('估算覆盖 6 / 8 Tasks')).toBeInTheDocument()
		expect(screen.getByText('已验证 7')).toBeInTheDocument()
		expect(screen.getByText('仅 Agent 报告 2')).toBeInTheDocument()
		expect(screen.getByText('87,953')).toBeInTheDocument()
		expect(screen.getByText('76.5%')).toBeInTheDocument()
		expect(screen.getByText('已采集 2 / 4 Runs')).toBeInTheDocument()
	})
})
