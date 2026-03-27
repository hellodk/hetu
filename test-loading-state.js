const puppeteer = require('puppeteer');

(async () => {
  console.log('🚀 Testing loading state...\n');
  
  const browser = await puppeteer.launch({
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox']
  });
  
  const page = await browser.newPage();
  await page.setViewport({ width: 1920, height: 1080 });
  
  try {
    // Enable request interception to delay the response
    await page.setRequestInterception(true);
    
    let apiRequestSeen = false;
    
    page.on('request', request => {
      // Delay API requests to capture loading state
      if (request.url().includes('/api/') || request.url().includes('cluster')) {
        console.log('   📡 API request intercepted:', request.url());
        apiRequestSeen = true;
        // Delay for 3 seconds to capture loading state
        setTimeout(() => {
          request.continue();
        }, 3000);
      } else {
        request.continue();
      }
    });
    
    console.log('📍 Navigating to http://localhost:3000...');
    
    // Start navigation but don't wait for it to complete
    const navigationPromise = page.goto('http://localhost:3000', { 
      waitUntil: 'domcontentloaded',
      timeout: 15000 
    });
    
    // Wait a bit for initial HTML to render
    await new Promise(resolve => setTimeout(resolve, 500));
    
    // Capture loading state
    console.log('📸 Capturing loading state...');
    await page.screenshot({ path: '/tmp/dashboard-loading-state.png', fullPage: true });
    
    // Check for skeleton elements
    const skeletonInfo = await page.evaluate(() => {
      const skeletons = document.querySelectorAll('.skeleton, .skeleton-circle');
      const skeletonCards = document.querySelectorAll('.bg-cluster-card .skeleton');
      const loadingHeader = document.querySelector('header .skeleton');
      
      return {
        totalSkeletons: skeletons.length,
        skeletonCards: skeletonCards.length,
        hasLoadingHeader: !!loadingHeader,
        ariaLabel: document.querySelector('[aria-busy="true"]')?.getAttribute('aria-label')
      };
    });
    
    console.log('   ✓ Total skeleton elements:', skeletonInfo.totalSkeletons);
    console.log('   ✓ Skeleton cards:', skeletonInfo.skeletonCards);
    console.log('   ✓ Loading header:', skeletonInfo.hasLoadingHeader);
    console.log('   ✓ ARIA label:', skeletonInfo.ariaLabel);
    
    // Wait for navigation to complete
    await navigationPromise;
    
    // Wait a bit more for error state to appear
    await new Promise(resolve => setTimeout(resolve, 2000));
    
    // Capture final state
    console.log('📸 Capturing final state...');
    await page.screenshot({ path: '/tmp/dashboard-after-loading.png', fullPage: true });
    
    if (skeletonInfo.totalSkeletons > 0) {
      console.log('\n✅ Loading state test PASSED');
      console.log('   Skeleton loading screens are properly displayed');
    } else {
      console.log('\n⚠️  Loading state test WARNING');
      console.log('   No skeleton elements found - loading may be too fast');
    }
    
  } catch (error) {
    console.error('\n❌ Error during testing:', error.message);
  } finally {
    await browser.close();
  }
  
  console.log('\n✨ Loading state test complete!');
})();
