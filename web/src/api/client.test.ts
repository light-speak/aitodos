import { afterEach, describe, expect, it, vi } from 'vitest'

import { getProjectProgress, getRunUsageSummary } from './client'

describe('getProjectProgress', () => {
	afterEach(() => vi.unstubAllGlobals())

	it('把尚无估算的 null 预测解析为未知', async () => {
		vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.resolve(Response.json({
			total_tasks: 2,
			accepted_tasks: 0,
			strict_percent: 0,
			estimated_tasks: 0,
			estimate_coverage: 0,
			total_points: 0,
			remaining_points: 0,
			forecast_percent: null,
			required_tests: 0,
			verified_passed_tests: 0,
			agent_reported_passed_tests: 0,
		}))))

		const progress = await getProjectProgress()
		expect(progress.total_tasks).toBe(2)
		expect(progress).not.toHaveProperty('forecast_percent')
	})
})

describe('getRunUsageSummary', () => {
	afterEach(() => vi.unstubAllGlobals())

	it('区分累计输入、缓存输入与未知峰值', async () => {
		vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.resolve(Response.json({
			total_runs: 2, runs_with_usage: 1,
			input_tokens: 71420, cached_input_tokens: 56320, uncached_input_tokens: 15100,
			cache_write_input_tokens: 0, output_tokens: 916, reasoning_output_tokens: 276,
			model_requests: null, peak_input_tokens: null,
			by_purpose: [{
				purpose: 'IMPLEMENTATION', total_runs: 2, runs_with_usage: 1,
				input_tokens: 71420, cached_input_tokens: 56320, uncached_input_tokens: 15100,
				cache_write_input_tokens: 0, output_tokens: 916, reasoning_output_tokens: 276,
				model_requests: null, peak_input_tokens: null,
			}],
		}))))

		const usage = await getRunUsageSummary()
		expect(usage.input_tokens).toBe(71420)
		expect(usage.peak_input_tokens).toBeUndefined()
		expect(usage.by_purpose[0]?.purpose).toBe('IMPLEMENTATION')
	})
})
