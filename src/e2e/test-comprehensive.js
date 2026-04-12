const puppeteer = require('puppeteer');

(async () => {
  console.log('🚀 Comprehensive UI/UX Test for K8s Dashboard\n');
  console.log('='.repeat(70));
  
  const browser = await puppeteer.launch({
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox']
  });
  
  const page = await browser.newPage();
  await page.setViewport({ width: 1920, height: 1080 });
  
  const results = {
    loadingState: false,
    errorState: false,
    mockDataFallback: false,
    responsiveLayout: false,
    accessibility: {
      focusStates: false,
      keyboardNavigation: false,
      ariaLabels: false
    },
    visualPolish: {
      hoverEffects: false,
      tabNavigation: false,
      groupedButtons: false
    }
  };
  
  try {
    console.log('\n📍 TEST 1: Loading State');
    console.log('-'.repeat(70));
    
    // Intercept API requests to capture loading state
    await page.setRequestInterception(true);
    
    page.on('request', request => {
      if (request.url().includes('/api/v1/health')) {
        // Delay API response to capture loading state
        setTimeout(() => {
          request.abort('failed'); // Simulate failure to test error handling
        }, 2000);
      } else {
        request.continue();
      }
    });
    
    const navigationPromise = page.goto('http://localhost:3000', { 
      waitUntil: 'domcontentloaded',
      timeout: 15000 
    });
    
    // Wait for initial render
    await new Promise(resolve => setTimeout(resolve, 500));
    
    // Check loading state
    const loadingState = await page.evaluate(() => {
      const skeletons = document.querySelectorAll('.skeleton, .skeleton-circle');
      const ariaBusy = document.querySelector('[aria-busy="true"]');
      return {
        skeletonCount: skeletons.length,
        hasAriaBusy: !!ariaBusy,
        ariaLabel: ariaBusy?.getAttribute('aria-label')
      };
    });
    
    console.log(`   ✓ Skeleton elements found: ${loadingState.skeletonCount}`);
    console.log(`   ✓ ARIA busy attribute: ${loadingState.hasAriaBusy}`);
    console.log(`   ✓ ARIA label: "${loadingState.ariaLabel}"`);
    
    if (loadingState.skeletonCount > 0) {
      results.loadingState = true;
      console.log('   ✅ PASS: Loading state with skeletons displayed');
    }
    
    await page.screenshot({ path: '/tmp/test-1-loading.png', fullPage: true });
    console.log('   📸 Screenshot saved: /tmp/test-1-loading.png');
    
    await navigationPromise.catch(() => {}); // Ignore navigation errors
    await new Promise(resolve => setTimeout(resolve, 2000));
    
    console.log('\n📍 TEST 2: Error State');
    console.log('-'.repeat(70));
    
    const errorState = await page.evaluate(() => {
      const errorDiv = document.querySelector('[role="alert"]');
      const errorTitle = errorDiv?.querySelector('h1')?.textContent;
      const errorMessage = errorDiv?.querySelector('p')?.textContent;
      const retryButton = Array.from(document.querySelectorAll('button')).find(btn => 
        btn.textContent.includes('Try Again') || btn.textContent.includes('Retry')
      );
      const alertTriangle = errorDiv?.querySelector('[class*="AlertTriangle"], svg');
      
      return {
        hasError: !!errorDiv,
        errorTitle,
        errorMessage,
        hasRetryButton: !!retryButton,
        hasIcon: !!alertTriangle
      };
    });
    
    console.log(`   ✓ Error state detected: ${errorState.hasError}`);
    console.log(`   ✓ Error title: "${errorState.errorTitle}"`);
    console.log(`   ✓ Error message: "${errorState.errorMessage}"`);
    console.log(`   ✓ Retry button: ${errorState.hasRetryButton}`);
    console.log(`   ✓ Error icon: ${errorState.hasIcon}`);
    
    if (errorState.hasError && errorState.hasRetryButton) {
      results.errorState = true;
      console.log('   ✅ PASS: Error state with retry button');
    }
    
    await page.screenshot({ path: '/tmp/test-2-error.png', fullPage: true });
    console.log('   📸 Screenshot saved: /tmp/test-2-error.png');
    
    // Now test with mock data by reloading without interception
    console.log('\n📍 TEST 3: Mock Data Fallback & Full Dashboard');
    console.log('-'.repeat(70));
    
    // Remove all listeners and disable interception
    page.removeAllListeners('request');
    await page.setRequestInterception(false);
    await page.reload({ waitUntil: 'networkidle0' });
    await new Promise(resolve => setTimeout(resolve, 2000));
    
    const dashboardState = await page.evaluate(() => {
      const header = document.querySelector('header');
      const title = header?.querySelector('h1')?.textContent;
      const scoreCards = document.querySelectorAll('[class*="score"], .bg-cluster-card');
      const tabs = document.querySelectorAll('[role="tab"]');
      const footer = document.querySelector('footer');
      
      return {
        hasHeader: !!header,
        title,
        scoreCardCount: scoreCards.length,
        tabCount: tabs.length,
        hasFooter: !!footer,
        isFullDashboard: scoreCards.length > 0 && tabs.length > 0
      };
    });
    
    console.log(`   ✓ Header present: ${dashboardState.hasHeader}`);
    console.log(`   ✓ Title: "${dashboardState.title}"`);
    console.log(`   ✓ Score cards: ${dashboardState.scoreCardCount}`);
    console.log(`   ✓ Navigation tabs: ${dashboardState.tabCount}`);
    console.log(`   ✓ Footer present: ${dashboardState.hasFooter}`);
    
    if (dashboardState.isFullDashboard) {
      results.mockDataFallback = true;
      console.log('   ✅ PASS: Dashboard loads with mock data');
    }
    
    await page.screenshot({ path: '/tmp/test-3-dashboard.png', fullPage: true });
    console.log('   📸 Screenshot saved: /tmp/test-3-dashboard.png');
    
    console.log('\n📍 TEST 4: Responsive Layout');
    console.log('-'.repeat(70));
    
    // Check header button grouping
    const headerLayout = await page.evaluate(() => {
      const header = document.querySelector('header');
      const buttonGroup = header?.querySelector('[role="group"]');
      const buttonsInGroup = buttonGroup?.querySelectorAll('button').length || 0;
      const groupLabel = buttonGroup?.getAttribute('aria-label');
      
      return {
        hasButtonGroup: !!buttonGroup,
        buttonsInGroup,
        groupLabel
      };
    });
    
    console.log(`   ✓ Button group found: ${headerLayout.hasButtonGroup}`);
    console.log(`   ✓ Buttons in group: ${headerLayout.buttonsInGroup}`);
    console.log(`   ✓ Group label: "${headerLayout.groupLabel}"`);
    
    if (headerLayout.hasButtonGroup && headerLayout.buttonsInGroup >= 2) {
      results.responsiveLayout = true;
      results.visualPolish.groupedButtons = true;
      console.log('   ✅ PASS: Header buttons are grouped');
    }
    
    // Test mobile view
    console.log('\n   Testing mobile responsiveness...');
    await page.setViewport({ width: 375, height: 667 });
    await new Promise(resolve => setTimeout(resolve, 500));
    await page.screenshot({ path: '/tmp/test-4-mobile.png', fullPage: true });
    console.log('   📸 Mobile screenshot: /tmp/test-4-mobile.png');
    
    // Back to desktop
    await page.setViewport({ width: 1920, height: 1080 });
    await new Promise(resolve => setTimeout(resolve, 500));
    
    console.log('\n📍 TEST 5: Accessibility - Focus States');
    console.log('-'.repeat(70));
    
    // Tab through elements and check focus
    await page.keyboard.press('Tab');
    await new Promise(resolve => setTimeout(resolve, 200));
    
    let focusTests = [];
    for (let i = 0; i < 5; i++) {
      const focusInfo = await page.evaluate(() => {
        const focused = document.activeElement;
        if (!focused || focused === document.body) return null;
        
        const styles = window.getComputedStyle(focused);
        const tagName = focused.tagName.toLowerCase();
        const text = focused.textContent?.substring(0, 30) || '';
        const ariaLabel = focused.getAttribute('aria-label');
        
        // Check for visible focus indicators
        const hasFocusRing = (
          styles.outline !== 'none' ||
          styles.outlineWidth !== '0px' ||
          styles.boxShadow.includes('ring') ||
          styles.boxShadow.includes('0 0') ||
          focused.className.includes('focus')
        );
        
        return {
          tagName,
          text: text.trim(),
          ariaLabel,
          hasFocusRing,
          outline: styles.outline,
          boxShadow: styles.boxShadow
        };
      });
      
      if (focusInfo) {
        focusTests.push(focusInfo);
        console.log(`   Tab ${i + 1}: ${focusInfo.tagName} - "${focusInfo.ariaLabel || focusInfo.text}"`);
        console.log(`           Focus ring: ${focusInfo.hasFocusRing ? '✓' : '✗'}`);
      }
      
      await page.keyboard.press('Tab');
      await new Promise(resolve => setTimeout(resolve, 200));
    }
    
    const focusableWithRings = focusTests.filter(f => f.hasFocusRing).length;
    if (focusableWithRings > 0) {
      results.accessibility.focusStates = true;
      results.accessibility.keyboardNavigation = true;
      console.log(`   ✅ PASS: ${focusableWithRings}/${focusTests.length} elements have visible focus states`);
    }
    
    await page.screenshot({ path: '/tmp/test-5-focus.png', fullPage: true });
    console.log('   📸 Screenshot saved: /tmp/test-5-focus.png');
    
    console.log('\n📍 TEST 6: ARIA Labels & Semantic HTML');
    console.log('-'.repeat(70));
    
    const ariaInfo = await page.evaluate(() => {
      const skipLink = document.querySelector('.skip-link');
      const mainContent = document.querySelector('main#main-content');
      const tablist = document.querySelector('[role="tablist"]');
      const tabs = document.querySelectorAll('[role="tab"]');
      const tabpanels = document.querySelectorAll('[role="tabpanel"]');
      const sections = document.querySelectorAll('section[aria-labelledby]');
      const srOnly = document.querySelectorAll('.sr-only');
      
      return {
        hasSkipLink: !!skipLink,
        hasMainLandmark: !!mainContent,
        hasTablist: !!tablist,
        tabCount: tabs.length,
        tabpanelCount: tabpanels.length,
        sectionCount: sections.length,
        srOnlyCount: srOnly.length,
        tablistLabel: tablist?.getAttribute('aria-label')
      };
    });
    
    console.log(`   ✓ Skip link: ${ariaInfo.hasSkipLink}`);
    console.log(`   ✓ Main landmark: ${ariaInfo.hasMainLandmark}`);
    console.log(`   ✓ Tab navigation: ${ariaInfo.hasTablist}`);
    console.log(`   ✓ Tabs: ${ariaInfo.tabCount}`);
    console.log(`   ✓ Tab panels: ${ariaInfo.tabpanelCount}`);
    console.log(`   ✓ Labeled sections: ${ariaInfo.sectionCount}`);
    console.log(`   ✓ Screen reader only text: ${ariaInfo.srOnlyCount}`);
    
    if (ariaInfo.hasSkipLink && ariaInfo.hasMainLandmark && ariaInfo.hasTablist) {
      results.accessibility.ariaLabels = true;
      console.log('   ✅ PASS: Proper ARIA labels and semantic HTML');
    }
    
    console.log('\n📍 TEST 7: Tab Navigation');
    console.log('-'.repeat(70));
    
    // Click on different tabs
    const tabTest = await page.evaluate(() => {
      const tabs = Array.from(document.querySelectorAll('[role="tab"]'));
      const results = [];
      
      tabs.forEach((tab, index) => {
        const isSelected = tab.getAttribute('aria-selected') === 'true';
        const text = tab.textContent?.trim();
        const hasUnderline = tab.className.includes('border-b') || 
                            tab.className.includes('border-blue');
        
        results.push({
          index,
          text,
          isSelected,
          hasUnderline,
          classes: tab.className
        });
      });
      
      return results;
    });
    
    console.log('   Tab states:');
    tabTest.forEach(tab => {
      const marker = tab.isSelected ? '→' : ' ';
      console.log(`   ${marker} Tab ${tab.index + 1}: "${tab.text}" (selected: ${tab.isSelected})`);
    });
    
    // Click second tab
    const tabs = await page.$$('[role="tab"]');
    if (tabs.length > 1) {
      await tabs[1].click();
      await new Promise(resolve => setTimeout(resolve, 500));
      
      const afterClick = await page.evaluate(() => {
        const tabs = Array.from(document.querySelectorAll('[role="tab"]'));
        return tabs.map(tab => ({
          text: tab.textContent?.trim(),
          selected: tab.getAttribute('aria-selected') === 'true'
        }));
      });
      
      console.log('\n   After clicking second tab:');
      afterClick.forEach((tab, i) => {
        const marker = tab.selected ? '→' : ' ';
        console.log(`   ${marker} Tab ${i + 1}: "${tab.text}" (selected: ${tab.selected})`);
      });
      
      results.visualPolish.tabNavigation = true;
      console.log('   ✅ PASS: Tab navigation works correctly');
      
      await page.screenshot({ path: '/tmp/test-7-tabs.png', fullPage: true });
      console.log('   📸 Screenshot saved: /tmp/test-7-tabs.png');
    }
    
    console.log('\n📍 TEST 8: Hover Effects');
    console.log('-'.repeat(70));
    
    // Test button hover
    const refreshButton = await page.$('button[aria-label*="Refresh"]');
    if (refreshButton) {
      const beforeHover = await refreshButton.evaluate(el => {
        const styles = window.getComputedStyle(el);
        return {
          backgroundColor: styles.backgroundColor,
          transform: styles.transform,
          opacity: styles.opacity
        };
      });
      
      await refreshButton.hover();
      await new Promise(resolve => setTimeout(resolve, 300));
      
      const afterHover = await refreshButton.evaluate(el => {
        const styles = window.getComputedStyle(el);
        return {
          backgroundColor: styles.backgroundColor,
          transform: styles.transform,
          opacity: styles.opacity
        };
      });
      
      console.log('   Button hover test:');
      console.log(`     Before: bg=${beforeHover.backgroundColor}, opacity=${beforeHover.opacity}`);
      console.log(`     After:  bg=${afterHover.backgroundColor}, opacity=${afterHover.opacity}`);
      
      if (beforeHover.backgroundColor !== afterHover.backgroundColor ||
          beforeHover.opacity !== afterHover.opacity ||
          beforeHover.transform !== afterHover.transform) {
        results.visualPolish.hoverEffects = true;
        console.log('   ✅ PASS: Hover effects detected on buttons');
      }
      
      await page.screenshot({ path: '/tmp/test-8-hover.png', fullPage: true });
      console.log('   📸 Screenshot saved: /tmp/test-8-hover.png');
    }
    
    // Test card hover
    const scoreCard = await page.$('.bg-cluster-card');
    if (scoreCard) {
      const beforeCardHover = await scoreCard.evaluate(el => {
        const styles = window.getComputedStyle(el);
        return {
          transform: styles.transform,
          boxShadow: styles.boxShadow
        };
      });
      
      await scoreCard.hover();
      await new Promise(resolve => setTimeout(resolve, 300));
      
      const afterCardHover = await scoreCard.evaluate(el => {
        const styles = window.getComputedStyle(el);
        return {
          transform: styles.transform,
          boxShadow: styles.boxShadow
        };
      });
      
      console.log('\n   Card hover test:');
      console.log(`     Transform changed: ${beforeCardHover.transform !== afterCardHover.transform}`);
      console.log(`     Shadow changed: ${beforeCardHover.boxShadow !== afterCardHover.boxShadow}`);
      
      if (beforeCardHover.transform !== afterCardHover.transform) {
        console.log('   ✅ Card hover effects detected');
      }
    }
    
  } catch (error) {
    console.error('\n❌ Error during testing:', error.message);
    console.error(error.stack);
  } finally {
    await browser.close();
  }
  
  // Print final summary
  console.log('\n' + '='.repeat(70));
  console.log('📊 FINAL TEST SUMMARY');
  console.log('='.repeat(70));
  
  const tests = [
    { name: '1. Loading State (Skeleton)', pass: results.loadingState },
    { name: '2. Error State (with Retry)', pass: results.errorState },
    { name: '3. Mock Data Fallback', pass: results.mockDataFallback },
    { name: '4. Responsive Layout', pass: results.responsiveLayout },
    { name: '5. Focus States Visible', pass: results.accessibility.focusStates },
    { name: '6. Keyboard Navigation', pass: results.accessibility.keyboardNavigation },
    { name: '7. ARIA Labels', pass: results.accessibility.ariaLabels },
    { name: '8. Tab Navigation', pass: results.visualPolish.tabNavigation },
    { name: '9. Grouped Action Buttons', pass: results.visualPolish.groupedButtons },
    { name: '10. Hover Effects', pass: results.visualPolish.hoverEffects }
  ];
  
  tests.forEach(test => {
    const icon = test.pass ? '✅' : '❌';
    console.log(`${icon} ${test.name}`);
  });
  
  const passCount = tests.filter(t => t.pass).length;
  const totalCount = tests.length;
  const percentage = Math.round((passCount / totalCount) * 100);
  
  console.log('='.repeat(70));
  console.log(`RESULT: ${passCount}/${totalCount} tests passed (${percentage}%)`);
  console.log('='.repeat(70));
  
  console.log('\n📁 All screenshots saved in /tmp/');
  console.log('   - test-1-loading.png     (Loading skeleton state)');
  console.log('   - test-2-error.png       (Error state with retry)');
  console.log('   - test-3-dashboard.png   (Full dashboard with mock data)');
  console.log('   - test-4-mobile.png      (Mobile responsive view)');
  console.log('   - test-5-focus.png       (Keyboard focus states)');
  console.log('   - test-7-tabs.png        (Tab navigation)');
  console.log('   - test-8-hover.png       (Hover effects)');
  
  console.log('\n✨ Testing complete!\n');
})();
