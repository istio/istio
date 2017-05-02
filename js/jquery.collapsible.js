/*!
* Adapted from collapsible.js 1.0.0
* https://github.com/jordnkr/collapsible
*/

    function highlightActive() {
      function stringEndsWith(str, endsWithString) {
        var index = str.indexOf(endsWithString);
        if (index >= 0 && str.length == endsWithString.length + index) {
          return true;
        } else {
          return false;
        }
      }

      // First have to invalidate old active item.
      $('.docs-side-nav a.active').removeClass('active');
      var currentLocation = window.location.hostname + window.location.pathname;
      $('.docs-side-nav li a[href]').each(function(index, element) {
        if (stringEndsWith(currentLocation, element.href.replace(/^.*\/\//,"").replace(/\:\d+/, ""))) {
          $(element).addClass('active');
        }
      });
    };


(function($, undefined) {
    $.fn.collapsible = function(effect, options) {
        var defaults = {
            accordionUpSpeed: 400,
            accordionDownSpeed: 400,
            collapseSpeed: 400,
            contentOpen: 0,
            arrowRclass: 'arrow-r',
            arrowDclass: 'arrow-d',
            animate: true
        };

        if (typeof effect === "object") {
            var settings = $.extend(defaults, effect);
        } else {
            var settings = $.extend(defaults, options);
        }

        return this.each(function() {
            if (settings.animate === false) {
                settings.accordionUpSpeed = 0;
                settings.accordionDownSpeed = 0;
                settings.collapseSpeed = 0;
            }

            var $thisEven = $(this).children(':even');
            var $thisOdd = $(this).children(':odd');
            var accord = 'accordion-active';
            

            switch (effect) {
              case 'accordion-open':
              /* FALLTHROUGH */
              case 'accordion':
                if (effect === 'accordion-open') {
                    $($thisEven[settings.contentOpen]).children(':first-child').toggleClass(settings.arrowRclass + ' ' + settings.arrowDclass);
                    $($thisOdd[settings.contentOpen]).show().addClass(accord);
                 }
                 $($thisEven).click(function() {
                   if ($(this).next().attr('class') === accord) {
                            $(this).next().slideUp(settings.accordionUpSpeed).removeClass(accord);
                            $(this).children(':first-child').toggleClass(settings.arrowRclass + ' ' + settings.arrowDclass);
                   } else {
                            $($thisEven).children().removeClass(settings.arrowDclass).addClass(settings.arrowRclass); 
                            $($thisOdd).slideUp(settings.accordionUpSpeed).removeClass(accord);
                            $(this).next().slideDown(settings.accordionDownSpeed).addClass(accord); 
                            $(this).children(':first-child').toggleClass(settings.arrowRclass + ' ' + settings.arrowDclass);              
                   }
                   });
                 break;
               case 'default-open':
               /* FALLTHROUGH */
               default:
                 // is everything open by default or do I have an active child?
                 if (effect === 'default-open'|| $(this).find("a.active").length) {
                   $($thisEven[settings.contentOpen]).toggleClass(settings.arrowRclass + ' ' + settings.arrowDclass);
                   $($thisOdd[settings.contentOpen]).show();
                 }
                 $($thisEven).click(function() {
                   $(this).toggleClass(settings.arrowRclass + ' ' + settings.arrowDclass);
                   $(this).next().slideToggle(settings.collapseSpeed);
                  });
                 break;
            }
        });
    };
})(jQuery);

