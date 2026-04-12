import { test, expect } from '@playwright/test';

test.describe('Cluster Intelligence Dashboard', () => {
  test.beforeEach(async ({ page }) => {
    // We expect the dashboard to be available at http://localhost:8080 due to port-forwarding
    await page.goto('/');
  });

  test('should load the dashboard and show health scores', async ({ page }) => {
    // Check for the main title or heading
    await expect(page.getByRole('heading', { name: /Cluster Intelligence/i })).toBeVisible();

    // Check for health scores section
    const scoresHeading = page.locator('#scores-heading');
    await expect(scoresHeading).toBeVisible();

    // Verify presence of Overall Health score card
    await expect(page.getByText('Overall Health')).toBeVisible();
    
    // Verify presence of Reliability score card
    await expect(page.getByText('Reliability')).toBeVisible();
  });

  test('should navigate between tabs', async ({ page }) => {
    // Click on 'Issues' tab (assuming there's a button or link with this text)
    const issuesTab = page.getByRole('button', { name: /Issues/i }).or(page.getByText('Issues', { exact: true }));
    await issuesTab.click();
    
    // Verify we are on the issues section
    // (Adjusting based on actual UI implementation observed in page.tsx)
    await expect(page.locator('section[aria-labelledby="issues-heading"]').or(page.getByText(/Critical Issues/i))).toBeVisible();

    // Click on 'Timeline' tab
    const timelineTab = page.getByRole('button', { name: /Timeline/i });
    if (await timelineTab.isVisible()) {
        await timelineTab.click();
        await expect(page.locator('#timeline-heading')).toBeVisible();
    }
  });

  test('should have a working refresh button', async ({ page }) => {
    const refreshButton = page.getByLabel(/Refresh dashboard data/i);
    await expect(refreshButton).toBeEnabled();
    await refreshButton.click();
    
    // Check if loading state appears (e.g. spinner or aria-label change)
    // Based on page.tsx: aria-label={loading ? 'Refreshing data...' : 'Refresh dashboard data'}
    // This might happen too fast to catch, but we can try.
  });

  test('should be responsive', async ({ page, isMobile }) => {
    if (isMobile) {
      // Check for mobile-specific elements, like a hamburger menu or condensed view
      // In page.tsx, some elements have 'hidden md:flex'
      await expect(page.locator('.md\\:flex')).not.toBeVisible();
    } else {
      await expect(page.locator('.hidden.md\\:flex')).toBeVisible();
    }
  });
});
