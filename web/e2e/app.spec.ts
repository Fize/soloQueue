import { test, expect } from '@playwright/test'

test.describe('Navigation', () => {
  test('loads the dashboard by default', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByText('Total Plans')).toBeVisible()
    await expect(page.getByText('Active Agents')).toBeVisible()
  })

  test('navigates to Plans page via sidebar', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: /plans/i }).click()
    await expect(page).toHaveURL('/plans')
    await expect(page.getByText('Plans Board')).toBeVisible()
  })

  test('navigates to Files page', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: /files/i }).click()
    await expect(page).toHaveURL('/files')
    await expect(page.getByText('Files')).toBeVisible()
  })

  test('navigates to Settings page', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: /settings/i }).click()
    await expect(page).toHaveURL('/settings/config')
  })
})

test.describe('Plans Board', () => {
  test('shows three columns', async ({ page }) => {
    await page.goto('/plans')
    await expect(page.getByText('Plans Board')).toBeVisible()
    await expect(page.getByText('Plan')).toBeVisible()
    await expect(page.getByText('Running')).toBeVisible()
    await expect(page.getByText('Done')).toBeVisible()
  })

  test('opens create plan dialog', async ({ page }) => {
    await page.goto('/plans')
    await page.getByRole('button', { name: /new plan/i }).click()
    await expect(page.getByText('New Plan')).toBeVisible()
    await expect(page.getByPlaceholder('Plan title')).toBeVisible()
  })

  test('shows validation error for empty plan title', async ({ page }) => {
    await page.goto('/plans')
    await page.getByRole('button', { name: /new plan/i }).click()
    await page.getByRole('button', { name: /create/i }).click()
    await expect(page.getByText('Title is required')).toBeVisible()
  })
})

test.describe('Dashboard', () => {
  test('displays stat cards', async ({ page }) => {
    await page.goto('/')
    const cards = ['Total Plans', 'Running', 'Completed', 'Active Agents']
    for (const card of cards) {
      await expect(page.getByText(card)).toBeVisible()
    }
  })
})
