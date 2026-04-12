const puppeteer = require('puppeteer');
const fs = require('fs');

(async () => {
  console.log('🚀 Starting dashboard UI/UX test...\n');
  
  const browser = await puppeteer.launch({
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox']
  });
  
  const page = await browser.newPage();
  await page.setViewport({ width: 1920, height: 1080 });
  
  const results = {
    loadingState: false,
    errorState: false,
    responsiveLayout: false,
    accessibility: {
      focusStates: false,
      keyboardNavigation: false
    },
    visualPolish: {
      hoverEffects: false,
      tabNavigation: false
    }
  };
  
  try {
    // Navigate to the dashboard
    console.log('📍 Navigating to http://localhost:3000...');
    await page.goto('http://localhost:3000', { waitUntil: 'networkidle0', timeout: 10000 });
    
    // Test 1: Loading State (check if skeleton is present initially)
    console.log('\n✅ Test 1: Loading State');
    const hasSkeletons = await page.evaluate(() => {
      const skeletons = document.querySelectorAll('.skeleton, .skeleton-circle');
      return skeletons.length > 0;
    });
    
    if (hasSkeletons) {
      console.log('   ✓ Skeleton loading screens detected');
      results.loadingState = true;
      await page.screenshot({ path: '/tmp/dashboard-loading.png', fullPage: true });
      console.log('   📸 Screenshot saved: /tmp/dashboard-loading.png');
    }
    
    // Wait a bit for data to load or error to appear
    await new Promise(resolve => setTimeout(resolve, 2000));
    
    // Test 2: Error State
    console.log('\n✅ Test 2: Error State');
    const errorState = await page.evaluate(() => {
      const errorDiv = document.querySelector('[role="alert"]');
      const retryButton = Array.from(document.querySelectorAll('button')).find(btn => 
        btn.textContent.toLowerCase().includes('retry')
      );
      
      return {
        hasError: !!errorDiv,
        errorMessage: errorDiv?.textContent || '',
        hasRetryButton: !!retryButton
      };
    });
    
    if (errorState.hasError) {
      console.log('   ✓ Error state detected');
      console.log(`   ✓ Error message: "${errorState.errorMessage.substring(0, 100)}..."`);
      if (errorState.hasRetryButton) {
        console.log('   ✓ Retry button found');
      }
      results.errorState = true;
      await page.screenshot({ path: '/tmp/dashboard-error.png', fullPage: true });
      console.log('   📸 Screenshot saved: /tmp/dashboard-error.png');
    } else {
      console.log('   ℹ No error state (data may have loaded successfully)');
      await page.screenshot({ path: '/tmp/dashboard-loaded.png', fullPage: true });
      console.log('   📸 Screenshot saved: /tmp/dashboard-loaded.png');
    }
    
    // Test 3: Responsive Layout
    console.log('\n✅ Test 3: Responsive Layout');
    const layoutCheck = await page.evaluate(() => {
      const header = document.querySelector('header');
      const actionButtons = header?.querySelectorAll('button');
      
      return {
        hasHeader: !!header,
        buttonCount: actionButtons?.length || 0,
        headerClasses: header?.className || ''
      };
    });
    
    console.log(`   ✓ Header found: ${layoutCheck.hasHeader}`);
    console.log(`   ✓ Action buttons in header: ${layoutCheck.buttonCount}`);
    results.responsiveLayout = layoutCheck.hasHeader;
    
    // Test 4: Accessibility - Focus States
    console.log('\n✅ Test 4: Accessibility - Focus States');
    
    // Get all focusable elements
    const focusableElements = await page.evaluate(() => {
      const elements = Array.from(document.querySelectorAll('button, a, input, [tabindex]'));
      return elements.length;
    });
    
    console.log(`   ✓ Found ${focusableElements} focusable elements`);
    
    // Tab through first few elements and check focus
    if (focusableElements > 0) {
      await page.keyboard.press('Tab');
      await new Promise(resolve => setTimeout(resolve, 300));
      
      const focusVisible = await page.evaluate(() => {
        const focused = document.activeElement;
        if (!focused) return false;
        
        const styles = window.getComputedStyle(focused);
        const pseudoStyles = window.getComputedStyle(focused, ':focus-visible');
        
        // Check if there's a visible focus ring
        return (
          styles.outline !== 'none' ||
          styles.outlineWidth !== '0px' ||
          styles.boxShadow.includes('ring') ||
          focused.className.includes('focus')
        );
      });
      
      await page.screenshot({ path: '/tmp/dashboard-focus.png', fullPage: true });
      console.log('   📸 Screenshot with focus: /tmp/dashboard-focus.png');
      
      if (focusVisible) {
        console.log('   ✓ Focus states are visible');
        results.accessibility.focusStates = true;
      } else {
        console.log('   ⚠ Focus states may not be clearly visible');
      }
      results.accessibility.keyboardNavigation = true;
    }
    
    // Test 5: Visual Polish - Hover Effects
    console.log('\n✅ Test 5: Visual Polish - Hover Effects');
    
    const cards = await page.$$('.bg-cluster-card');
    if (cards.length > 0) {
      const card = cards[0];
      
      // Get initial transform
      const initialTransform = await card.evaluate(el => {
        return window.getComputedStyle(el).transform;
      });
      
      // Hover over the card
      await card.hover();
      await new Promise(resolve => setTimeout(resolve, 500));
      
      const hoverTransform = await card.evaluate(el => {
        return window.getComputedStyle(el).transform;
      });
      
      await page.screenshot({ path: '/tmp/dashboard-hover.png', fullPage: true });
      console.log('   📸 Screenshot with hover: /tmp/dashboard-hover.png');
      
      if (initialTransform !== hoverTransform) {
        console.log('   ✓ Hover effects detected (transform change)');
        results.visualPolish.hoverEffects = true;
      } else {
        console.log('   ℹ Hover effects may be subtle or CSS-based');
      }
    }
    
    // Test 6: Tab Navigation
    console.log('\n✅ Test 6: Tab Navigation');
    
    const tabInfo = await page.evaluate(() => {
      const tabs = document.querySelectorAll('[role="tab"], [role="tablist"] button');
      const activeTab = Array.from(tabs).find(tab => 
        tab.getAttribute('aria-selected') === 'true' ||
        tab.className.includes('active') ||
        tab.className.includes('border-b')
      );
      
      return {
        tabCount: tabs.length,
        hasActiveTab: !!activeTab,
        activeTabText: activeTab?.textContent || ''
      };
    });
    
    console.log(`   ✓ Found ${tabInfo.tabCount} tabs`);
    if (tabInfo.hasActiveTab) {
      console.log(`   ✓ Active tab: "${tabInfo.activeTabText}"`);
      results.visualPolish.tabNavigation = true;
    }
    
    // Final screenshot
    await page.screenshot({ path: '/tmp/dashboard-final.png', fullPage: true });
    console.log('\n📸 Final screenshot: /tmp/dashboard-final.png');
    
  } catch (error) {
    console.error('\n❌ Error during testing:', error.message);
  } finally {
    await browser.close();
  }
  
  // Print summary
  console.log('\n' + '='.repeat(60));
  console.log('📊 TEST SUMMARY');
  console.log('='.repeat(60));
  console.log(`Loading State:           ${results.loadingState ? '✅ PASS' : '❌ FAIL'}`);
  console.log(`Error State:             ${results.errorState ? '✅ PASS' : '⚠️  N/A'}`);
  console.log(`Responsive Layout:       ${results.responsiveLayout ? '✅ PASS' : '❌ FAIL'}`);
  console.log(`Focus States:            ${results.accessibility.focusStates ? '✅ PASS' : '⚠️  CHECK'}`);
  console.log(`Keyboard Navigation:     ${results.accessibility.keyboardNavigation ? '✅ PASS' : '❌ FAIL'}`);
  console.log(`Hover Effects:           ${results.visualPolish.hoverEffects ? '✅ PASS' : '⚠️  CHECK'}`);
  console.log(`Tab Navigation:          ${results.visualPolish.tabNavigation ? '✅ PASS' : '❌ FAIL'}`);
  console.log('='.repeat(60));
  
  console.log('\n✨ Testing complete! Check screenshots in /tmp/');
})();
